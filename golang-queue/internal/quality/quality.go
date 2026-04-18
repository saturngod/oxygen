package quality

type Rendition struct {
	Value        string
	Width        int
	Height       int
	VideoBitrate int
	AudioBitrate int
}

var Map = map[string]Rendition{
	"240p":  {Value: "240p", Width: 352, Height: 240, VideoBitrate: 600, AudioBitrate: 64},
	"360p":  {Value: "360p", Width: 640, Height: 360, VideoBitrate: 800, AudioBitrate: 96},
	"480p":  {Value: "480p", Width: 842, Height: 480, VideoBitrate: 1400, AudioBitrate: 128},
	"720p":  {Value: "720p", Width: 1280, Height: 720, VideoBitrate: 2800, AudioBitrate: 128},
	"1080p": {Value: "1080p", Width: 1920, Height: 1080, VideoBitrate: 5000, AudioBitrate: 192},
	"1440p": {Value: "1440p", Width: 2560, Height: 1440, VideoBitrate: 8000, AudioBitrate: 192},
	"2160p": {Value: "2160p", Width: 3840, Height: 2160, VideoBitrate: 25000, AudioBitrate: 192},
}

func Get(key string) (Rendition, bool) {
	r, ok := Map[key]
	return r, ok
}
