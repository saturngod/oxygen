package server

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bluenviron/gohlslib/v2/pkg/playlist"
)

type hlsRenditionState struct {
	PlaylistPath string
	SegmentCount int
	First        time.Time
	Latest       time.Time
}

type hlsOutputState struct {
	Renditions map[string]hlsRenditionState
}

func inspectHLSOutput(root string, minimumSegments int) (hlsOutputState, error) {
	masterPath := filepath.Join(root, "index.m3u8")
	masterBytes, err := os.ReadFile(masterPath)
	if err != nil {
		return hlsOutputState{}, err
	}
	parsed, err := playlist.Unmarshal(masterBytes)
	if err != nil {
		return hlsOutputState{}, fmt.Errorf("parse master playlist: %w", err)
	}
	master, ok := parsed.(*playlist.Multivariant)
	if !ok {
		return hlsOutputState{}, fmt.Errorf("index.m3u8 is not a multivariant playlist")
	}

	references := make(map[string]struct{})
	for _, variant := range master.Variants {
		references[variant.URI] = struct{}{}
	}
	for _, rendition := range master.Renditions {
		if rendition.URI == nil {
			continue
		}
		if rendition.Type == playlist.MultivariantRenditionTypeAudio || rendition.Type == playlist.MultivariantRenditionTypeVideo {
			references[*rendition.URI] = struct{}{}
		}
	}
	if len(references) == 0 {
		return hlsOutputState{}, fmt.Errorf("master playlist has no playable renditions")
	}

	state := hlsOutputState{Renditions: make(map[string]hlsRenditionState, len(references))}
	for reference := range references {
		playlistPath, err := resolveHLSReference(root, root, reference)
		if err != nil {
			return hlsOutputState{}, err
		}
		mediaBytes, err := os.ReadFile(playlistPath)
		if err != nil {
			return hlsOutputState{}, err
		}
		parsedMedia, err := playlist.Unmarshal(mediaBytes)
		if err != nil {
			return hlsOutputState{}, fmt.Errorf("parse media playlist %s: %w", reference, err)
		}
		media, ok := parsedMedia.(*playlist.Media)
		if !ok {
			return hlsOutputState{}, fmt.Errorf("referenced playlist %s is not a media playlist", reference)
		}
		if media.Map == nil {
			return hlsOutputState{}, fmt.Errorf("media playlist %s has no EXT-X-MAP", reference)
		}
		if _, err := requireNonemptyHLSFile(root, filepath.Dir(playlistPath), media.Map.URI); err != nil {
			return hlsOutputState{}, fmt.Errorf("media playlist %s init: %w", reference, err)
		}
		if len(media.Segments) < minimumSegments {
			return hlsOutputState{}, fmt.Errorf("media playlist %s has %d completed segments, need %d", reference, len(media.Segments), minimumSegments)
		}

		var first time.Time
		var latest time.Time
		for _, segment := range media.Segments {
			segmentPath, err := requireNonemptyHLSFile(root, filepath.Dir(playlistPath), segment.URI)
			if err != nil {
				return hlsOutputState{}, fmt.Errorf("media playlist %s segment: %w", reference, err)
			}
			info, err := os.Stat(segmentPath)
			if err != nil {
				return hlsOutputState{}, err
			}
			if info.ModTime().After(latest) {
				latest = info.ModTime()
			}
			if first.IsZero() || info.ModTime().Before(first) {
				first = info.ModTime()
			}
		}
		state.Renditions[reference] = hlsRenditionState{
			PlaylistPath: playlistPath,
			SegmentCount: len(media.Segments),
			First:        first,
			Latest:       latest,
		}
	}

	return state, nil
}

func requireNonemptyHLSFile(root string, base string, reference string) (string, error) {
	resolved, err := resolveHLSReference(root, base, reference)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return "", fmt.Errorf("%s is not a nonempty regular file", reference)
	}

	return resolved, nil
}

func resolveHLSReference(root string, base string, reference string) (string, error) {
	parsed, err := url.Parse(reference)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return "", fmt.Errorf("invalid HLS reference %q", reference)
	}
	decoded, err := url.PathUnescape(parsed.Path)
	if err != nil || decoded == "" || strings.HasPrefix(decoded, "/") || strings.Contains(decoded, "\\") {
		return "", fmt.Errorf("invalid HLS reference %q", reference)
	}
	resolved := filepath.Join(base, filepath.FromSlash(decoded))
	if resolved != root && !strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("HLS reference escapes stream directory: %q", reference)
	}

	return resolved, nil
}

func hlsOutputReady(root string, minimumSegments int) bool {
	_, err := inspectHLSOutput(root, minimumSegments)
	return err == nil
}

func waitForHLSReady(
	ctx context.Context,
	root string,
	minimumSegments int,
	readerDone <-chan struct{},
	readerError func() error,
	outputFailure func() error,
) (hlsOutputState, error) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		state, err := inspectHLSOutput(root, minimumSegments)
		if err == nil {
			select {
			case <-readerDone:
				if failure := outputFailure(); failure != nil {
					return hlsOutputState{}, failure
				}
				return hlsOutputState{}, fmt.Errorf("RTMP input ended before HLS became ready: %w", readerError())
			default:
			}
			if failure := outputFailure(); failure != nil {
				return hlsOutputState{}, failure
			}
			return state, nil
		}

		select {
		case <-ctx.Done():
			return hlsOutputState{}, fmt.Errorf("HLS startup did not complete: %w", ctx.Err())
		case <-readerDone:
			if failure := outputFailure(); failure != nil {
				return hlsOutputState{}, failure
			}
			return hlsOutputState{}, fmt.Errorf("RTMP input ended before HLS became ready: %w", readerError())
		case <-ticker.C:
			if failure := outputFailure(); failure != nil {
				return hlsOutputState{}, failure
			}
		}
	}
}
