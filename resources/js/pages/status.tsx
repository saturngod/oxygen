import { Head, usePoll } from '@inertiajs/react';
import {
    AlertCircle,
    CheckCircle2,
    Clock,
    Copy,
    Loader2,
    Video,
} from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import Heading from '@/components/heading';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
    Dialog,
    DialogClose,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table';

type HlsErrorData = {
    details?: string;
    fatal?: boolean;
};

type HlsInstance = {
    attachMedia: (media: HTMLMediaElement) => void;
    destroy: () => void;
    loadSource: (src: string) => void;
    on: (
        event: string,
        callback: (event: string, data: HlsErrorData) => void,
    ) => void;
};

type HlsConstructor = {
    Events: {
        ERROR: string;
        MANIFEST_PARSED: string;
    };
    isSupported: () => boolean;
    new (config?: Record<string, unknown>): HlsInstance;
};

declare global {
    interface Window {
        Hls?: HlsConstructor;
    }
}

const HLS_SCRIPT_SRC =
    'https://cdn.jsdelivr.net/npm/hls.js@1.6.16/dist/hls.min.js';

type FileStatus = 'uploaded' | 'progress' | 'success' | 'failed';

type FileProfile = {
    id: string;
    name: string;
    qualities: string[];
};

type FileItem = {
    id: string;
    title: string;
    file_name: string | null;
    source_url: string | null;
    streaming_url: string | null;
    status: FileStatus;
    progress: number;
    size: number;
    tags: string[];
    created_at: string | null;
    profiles: FileProfile[];
};

type Props = {
    files: FileItem[];
};

const statusStyles: Record<FileStatus, string> = {
    uploaded:
        'bg-blue-100 text-blue-700 dark:bg-blue-500/20 dark:text-blue-200',
    progress:
        'bg-amber-100 text-amber-700 dark:bg-amber-500/20 dark:text-amber-200',
    success:
        'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-200',
    failed: 'bg-red-100 text-red-700 dark:bg-red-500/20 dark:text-red-200',
};

const statusIcons: Record<FileStatus, React.ReactNode> = {
    uploaded: <Clock className="size-3" />,
    progress: <Loader2 className="size-3 animate-spin" />,
    success: <CheckCircle2 className="size-3" />,
    failed: <AlertCircle className="size-3" />,
};

const statusLabel: Record<FileStatus, string> = {
    uploaded: 'Queued',
    progress: 'Transcoding',
    success: 'Complete',
    failed: 'Failed',
};

function formatSize(bytes: number): string {
    if (!bytes) {
        return '—';
    }

    const units = ['B', 'KB', 'MB', 'GB'];
    let size = bytes;

    let unit = 0;

    while (size >= 1024 && unit < units.length - 1) {
        size /= 1024;
        unit++;
    }

    return `${size.toFixed(size >= 10 || unit === 0 ? 0 : 1)} ${units[unit]}`;
}

function formatDate(iso: string | null): string {
    if (!iso) {
        return '—';
    }

    return new Date(iso).toLocaleString();
}

function ProgressCell({ file }: { file: FileItem }) {
    if (file.status === 'progress') {
        return (
            <div className="flex min-w-[140px] items-center gap-2">
                <div className="h-2 flex-1 overflow-hidden rounded-full bg-muted">
                    <div
                        className="h-full rounded-full bg-amber-500 transition-all duration-500"
                        style={{ width: `${file.progress}%` }}
                    />
                </div>
                <span className="w-8 text-right text-xs text-muted-foreground tabular-nums">
                    {file.progress}%
                </span>
            </div>
        );
    }

    if (file.status === 'success') {
        return (
            <span className="text-xs text-emerald-600 dark:text-emerald-400">
                100%
            </span>
        );
    }

    return <span className="text-xs text-muted-foreground">—</span>;
}

function loadHlsScript(): Promise<void> {
    if (window.Hls) {
        return Promise.resolve();
    }

    return new Promise((resolve, reject) => {
        const existing = document.querySelector<HTMLScriptElement>(
            `script[src="${HLS_SCRIPT_SRC}"]`,
        );
        const script = existing ?? document.createElement('script');

        script.addEventListener('load', () => resolve(), { once: true });
        script.addEventListener(
            'error',
            () => reject(new Error('Unable to load the HLS player.')),
            { once: true },
        );

        if (!existing) {
            script.src = HLS_SCRIPT_SRC;
            script.async = true;
            document.head.appendChild(script);
        }
    });
}

function HlsPlayer({ src }: { src: string }) {
    const videoRef = useRef<HTMLVideoElement | null>(null);
    const [error, setError] = useState<string | null>(null);

    useEffect(() => {
        const video = videoRef.current;
        let cancelled = false;
        let hls: HlsInstance | null = null;

        if (!video || !src) {
            return;
        }

        setError(null);

        const handleVideoError = () => {
            setError(
                'Playback failed. Check that the signed URL is valid and the HLS files allow cross-origin requests.',
            );
        };

        video.addEventListener('error', handleVideoError);

        if (video.canPlayType('application/vnd.apple.mpegurl')) {
            video.src = src;
            video.load();

            return () => {
                video.removeEventListener('error', handleVideoError);
                video.removeAttribute('src');
                video.load();
            };
        }

        const attachHls = async () => {
            await loadHlsScript();

            if (cancelled || !window.Hls?.isSupported()) {
                if (!cancelled) {
                    setError('HLS playback is not supported in this browser.');
                }

                return;
            }

            hls = new window.Hls();
            hls.on(window.Hls.Events.ERROR, (_event, data) => {
                if (data.fatal) {
                    setError(
                        data.details
                            ? `Playback failed: ${data.details}`
                            : 'Playback failed while loading the HLS stream.',
                    );
                }
            });
            hls.loadSource(src);
            hls.attachMedia(video);
        };

        void attachHls().catch((reason: unknown) => {
            if (!cancelled) {
                setError(
                    reason instanceof Error
                        ? reason.message
                        : 'Unable to initialize HLS playback.',
                );
            }
        });

        return () => {
            cancelled = true;
            video.removeEventListener('error', handleVideoError);
            hls?.destroy();
        };
    }, [src]);

    return (
        <div className="space-y-2">
            <div className="aspect-video overflow-hidden rounded-lg border bg-black">
                <video
                    ref={videoRef}
                    controls
                    playsInline
                    className="h-full w-full"
                />
            </div>
            {error && (
                <p className="text-xs text-destructive" role="alert">
                    {error}
                </p>
            )}
        </div>
    );
}

function PlaybackTester({ defaultUrl }: { defaultUrl: string }) {
    const [url, setUrl] = useState(defaultUrl);
    const [playerUrl, setPlayerUrl] = useState(defaultUrl);

    const loadStream = (event: React.FormEvent<HTMLFormElement>) => {
        event.preventDefault();
        setPlayerUrl(url.trim());
    };

    return (
        <div className="space-y-3 rounded-lg border bg-muted/20 p-3">
            <div className="space-y-1">
                <h3 className="text-sm font-medium">Test HLS playback</h3>
                <p className="text-xs text-muted-foreground">
                    Use the default stream URL or paste a complete signed .m3u8
                    URL.
                </p>
            </div>
            <form className="flex gap-2" onSubmit={loadStream}>
                <div className="min-w-0 flex-1 space-y-1.5">
                    <Label htmlFor="hls-playback-url">Playback URL</Label>
                    <Input
                        id="hls-playback-url"
                        type="url"
                        value={url}
                        onChange={(event) => setUrl(event.target.value)}
                        placeholder="https://example.com/master.m3u8?signature=..."
                        required
                    />
                </div>
                <Button type="submit" className="self-end">
                    Load stream
                </Button>
            </form>
            {playerUrl && <HlsPlayer src={playerUrl} />}
        </div>
    );
}

export default function Status({ files }: Props) {
    const [fileDetails, setFileDetails] = useState<FileItem | null>(null);

    const hasActive = files.some(
        (f) => f.status === 'progress' || f.status === 'uploaded',
    );

    usePoll(
        3000,
        {
            only: ['files'],
        },
        {
            autoStart: hasActive,
            keepAlive: false,
        },
    );

    const activeCount = files.filter((f) => f.status === 'progress').length;
    const queuedCount = files.filter((f) => f.status === 'uploaded').length;

    return (
        <>
            <Head title="Status" />
            <div className="flex h-full flex-1 flex-col gap-4 p-4">
                <Heading
                    variant="page"
                    title="Status"
                    description="Transcode status and job progress."
                />

                {(activeCount > 0 || queuedCount > 0) && (
                    <div className="flex items-center gap-3 text-sm">
                        {activeCount > 0 && (
                            <div className="flex items-center gap-1.5 text-amber-600 dark:text-amber-400">
                                <Loader2 className="size-3.5 animate-spin" />
                                <span>
                                    {activeCount}{' '}
                                    {activeCount === 1 ? 'video' : 'videos'}{' '}
                                    transcoding
                                </span>
                            </div>
                        )}
                        {queuedCount > 0 && (
                            <div className="flex items-center gap-1.5 text-blue-600 dark:text-blue-400">
                                <Clock className="size-3.5" />
                                <span>{queuedCount} queued</span>
                            </div>
                        )}
                    </div>
                )}

                {files.length === 0 ? (
                    <div className="flex flex-1 flex-col items-center justify-center gap-3 rounded-lg border border-dashed p-12 text-center">
                        <Video className="size-10 text-muted-foreground/50" />
                        <p className="text-sm text-muted-foreground">
                            No videos yet. Upload a video to see its transcode
                            progress here.
                        </p>
                    </div>
                ) : (
                    <div className="relative flex-1 overflow-hidden rounded-lg border">
                        <Table>
                            <TableHeader>
                                <TableRow>
                                    <TableHead className="w-[40%]">
                                        Title
                                    </TableHead>
                                    <TableHead>Profile</TableHead>
                                    <TableHead>Status</TableHead>
                                    <TableHead className="w-[180px]">
                                        Progress
                                    </TableHead>
                                    <TableHead>Size</TableHead>
                                    <TableHead>Created</TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {files.map((file) => (
                                    <TableRow
                                        key={file.id}
                                        onClick={() => setFileDetails(file)}
                                        className="cursor-pointer"
                                    >
                                        <TableCell>
                                            <div className="flex flex-col gap-0.5">
                                                <span className="font-medium">
                                                    {file.title}
                                                </span>
                                                {file.file_name && (
                                                    <span className="text-xs text-muted-foreground">
                                                        {file.file_name}
                                                    </span>
                                                )}
                                            </div>
                                        </TableCell>
                                        <TableCell>
                                            {file.profiles.length > 0 ? (
                                                <div className="flex flex-col gap-0.5">
                                                    {file.profiles.map((p) => (
                                                        <span
                                                            key={p.id}
                                                            className="text-xs"
                                                        >
                                                            {p.name}
                                                        </span>
                                                    ))}
                                                </div>
                                            ) : (
                                                <span className="text-xs text-muted-foreground">
                                                    —
                                                </span>
                                            )}
                                        </TableCell>
                                        <TableCell>
                                            <Badge
                                                variant="secondary"
                                                className={`gap-1 ${statusStyles[file.status]}`}
                                            >
                                                {statusIcons[file.status]}
                                                {statusLabel[file.status]}
                                            </Badge>
                                        </TableCell>
                                        <TableCell>
                                            <ProgressCell file={file} />
                                        </TableCell>
                                        <TableCell className="text-xs">
                                            {formatSize(file.size)}
                                        </TableCell>
                                        <TableCell className="text-xs text-muted-foreground">
                                            {formatDate(file.created_at)}
                                        </TableCell>
                                    </TableRow>
                                ))}
                            </TableBody>
                        </Table>
                    </div>
                )}
            </div>

            <Dialog
                open={fileDetails !== null}
                onOpenChange={(open) => {
                    if (!open) {
                        setFileDetails(null);
                    }
                }}
            >
                <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-3xl">
                    <DialogHeader>
                        <DialogTitle>
                            {fileDetails?.title ?? 'Media'}
                        </DialogTitle>
                        <DialogDescription>
                            {fileDetails?.file_name ??
                                fileDetails?.source_url ??
                                '—'}
                        </DialogDescription>
                    </DialogHeader>

                    {fileDetails && (
                        <div className="space-y-4">
                            {fileDetails.streaming_url && (
                                <PlaybackTester
                                    key={fileDetails.id}
                                    defaultUrl={fileDetails.streaming_url}
                                />
                            )}

                            <div className="grid grid-cols-[8rem_1fr] gap-x-4 gap-y-2">
                                <span className="text-xs text-muted-foreground">
                                    Status
                                </span>
                                <div className="space-y-1.5">
                                    <Badge
                                        variant="secondary"
                                        className={`gap-1 ${statusStyles[fileDetails.status]}`}
                                    >
                                        {statusIcons[fileDetails.status]}
                                        {statusLabel[fileDetails.status]}
                                    </Badge>
                                    {fileDetails.status === 'progress' && (
                                        <div className="flex items-center gap-2">
                                            <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
                                                <div
                                                    className="h-full bg-amber-500 transition-all"
                                                    style={{
                                                        width: `${fileDetails.progress}%`,
                                                    }}
                                                />
                                            </div>
                                            <span className="text-[10px] text-muted-foreground tabular-nums">
                                                {fileDetails.progress}%
                                            </span>
                                        </div>
                                    )}
                                </div>

                                <span className="text-xs text-muted-foreground">
                                    Size
                                </span>
                                <span className="text-xs">
                                    {formatSize(fileDetails.size)}
                                </span>

                                <span className="text-xs text-muted-foreground">
                                    Created
                                </span>
                                <span className="text-xs">
                                    {formatDate(fileDetails.created_at)}
                                </span>

                                <span className="text-xs text-muted-foreground">
                                    Tags
                                </span>
                                <div className="flex flex-wrap gap-1">
                                    {fileDetails.tags.length === 0 ? (
                                        <span className="text-xs text-muted-foreground">
                                            —
                                        </span>
                                    ) : (
                                        fileDetails.tags.map((tag) => (
                                            <Badge key={tag} variant="outline">
                                                {tag}
                                            </Badge>
                                        ))
                                    )}
                                </div>

                                <span className="text-xs text-muted-foreground">
                                    Streaming URL
                                </span>
                                <div className="min-w-0">
                                    {fileDetails.streaming_url ? (
                                        <div className="flex items-center gap-2">
                                            <a
                                                href={fileDetails.streaming_url}
                                                target="_blank"
                                                rel="noopener noreferrer"
                                                className="truncate text-xs text-primary underline underline-offset-2"
                                            >
                                                {fileDetails.streaming_url}
                                            </a>
                                            <Button
                                                type="button"
                                                variant="ghost"
                                                size="icon-sm"
                                                aria-label="Copy streaming URL"
                                                onClick={() =>
                                                    navigator.clipboard.writeText(
                                                        fileDetails.streaming_url!,
                                                    )
                                                }
                                            >
                                                <Copy className="size-3.5" />
                                            </Button>
                                        </div>
                                    ) : (
                                        <span className="text-xs text-muted-foreground">
                                            Not ready yet
                                        </span>
                                    )}
                                </div>
                            </div>

                            <div className="space-y-2">
                                <h3 className="text-xs font-medium text-muted-foreground">
                                    Transcode profiles
                                </h3>
                                {fileDetails.profiles.length === 0 ? (
                                    <p className="rounded-md border border-dashed px-3 py-2 text-xs text-muted-foreground">
                                        No profile recorded for this upload.
                                    </p>
                                ) : (
                                    <div className="space-y-2">
                                        {fileDetails.profiles.map((profile) => (
                                            <div
                                                key={profile.id}
                                                className="rounded-md border bg-card px-3 py-2"
                                            >
                                                <div className="flex items-center justify-between gap-2">
                                                    <span className="text-xs font-medium">
                                                        {profile.name}
                                                    </span>
                                                    <span className="text-[10px] text-muted-foreground">
                                                        {
                                                            profile.qualities
                                                                .length
                                                        }{' '}
                                                        rendition
                                                        {profile.qualities
                                                            .length === 1
                                                            ? ''
                                                            : 's'}
                                                    </span>
                                                </div>
                                                <div className="mt-2 flex flex-wrap gap-1">
                                                    {profile.qualities.map(
                                                        (quality) => (
                                                            <Badge
                                                                key={quality}
                                                                variant="secondary"
                                                            >
                                                                {quality}
                                                            </Badge>
                                                        ),
                                                    )}
                                                </div>
                                            </div>
                                        ))}
                                    </div>
                                )}
                            </div>
                        </div>
                    )}

                    <DialogFooter>
                        <DialogClose asChild>
                            <Button variant="secondary">Close</Button>
                        </DialogClose>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </>
    );
}

Status.layout = {
    breadcrumbs: [
        {
            title: 'Status',
            href: '/status',
        },
    ],
};
