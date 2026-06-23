import { Form, Head, setLayoutProps, usePoll } from '@inertiajs/react';
import {
    Copy,
    Eye,
    KeyRound,
    Link as LinkIcon,
    Radio,
    RotateCcw,
    Settings,
    ShieldOff,
    Video,
} from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import OrganizationLiveStreamsController from '@/actions/App/Http/Controllers/Admin/OrganizationLiveStreamsController';
import Heading from '@/components/heading';
import InputError from '@/components/input-error';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
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
import {
    index as indexOrgLiveStreams,
    show as showOrgLiveStream,
} from '@/routes/admin/organizations/live-streams';

type HlsInstance = {
    loadSource: (src: string) => void;
    attachMedia: (media: HTMLMediaElement) => void;
    on: (event: string, callback: () => void) => void;
    destroy: () => void;
};

type HlsConstructor = {
    Events: {
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

type LiveStreamStatus =
    | 'idle'
    | 'live'
    | 'offline'
    | 'restarting'
    | 'failed'
    | 'disabled';

type StreamSession = {
    id: string;
    status: string;
    settings_version: number;
    recording_enabled: boolean;
    hls_url: string | null;
    recording_path: string | null;
    current_viewers: number;
    peak_viewers: number;
    unique_viewers: number;
    playlist_requests: number;
    segment_requests: number;
    started_at: string | null;
    ended_at: string | null;
    error_message: string | null;
};

type ViewerRollup = {
    minute: string | null;
    current_viewers: number;
    unique_viewers_seen: number;
    playlist_requests: number;
    segment_requests: number;
};

type LiveStream = {
    id: string;
    title: string;
    public_id: string;
    stream_key: string;
    stream_path: string;
    status: LiveStreamStatus;
    status_label: string;
    recording_enabled: boolean;
    restart_required: boolean;
    settings_version: number;
    rtmp_url: string;
    hls_url: string;
    last_started_at: string | null;
    last_ended_at: string | null;
    current_session: StreamSession | null;
    recent_sessions: StreamSession[];
    viewer_rollups: ViewerRollup[];
};

type Props = {
    organization: {
        id: string;
        name: string;
    };
    liveStream: LiveStream;
};

const statusClasses: Record<LiveStreamStatus, string> = {
    idle: 'bg-slate-100 text-slate-700 dark:bg-slate-500/20 dark:text-slate-200',
    live: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-200',
    offline: 'bg-zinc-100 text-zinc-700 dark:bg-zinc-500/20 dark:text-zinc-200',
    restarting:
        'bg-amber-100 text-amber-700 dark:bg-amber-500/20 dark:text-amber-200',
    failed: 'bg-red-100 text-red-700 dark:bg-red-500/20 dark:text-red-200',
    disabled:
        'bg-muted text-muted-foreground dark:bg-muted dark:text-muted-foreground',
};

function formatDate(value: string | null): string {
    return value ? new Date(value).toLocaleString() : '-';
}

function CopyField({
    label,
    value,
    secret = false,
}: {
    label: string;
    value: string;
    secret?: boolean;
}) {
    const [copied, setCopied] = useState(false);

    const copy = async () => {
        await navigator.clipboard.writeText(value);
        setCopied(true);
        window.setTimeout(() => setCopied(false), 1200);
    };

    return (
        <div className="grid gap-2">
            <Label>{label}</Label>
            <div className="flex min-w-0 gap-2">
                <Input
                    value={value}
                    type={secret ? 'password' : 'text'}
                    readOnly
                    className="font-mono"
                />
                <Button
                    type="button"
                    variant="outline"
                    size="icon"
                    onClick={copy}
                    aria-label={`Copy ${label}`}
                >
                    <Copy className="size-3.5" />
                </Button>
            </div>
            {copied && <span className="text-xs text-emerald-600">Copied</span>}
        </div>
    );
}

function LivePlayer({ src, isLive }: { src: string; isLive: boolean }) {
    const videoRef = useRef<HTMLVideoElement | null>(null);

    useEffect(() => {
        const video = videoRef.current;
        let cancelled = false;
        let hls: HlsInstance | null = null;

        if (!video || !src) {
            return;
        }

        const seekToLiveEdge = (force = false) => {
            if (video.seekable.length === 0) {
                return;
            }

            const edge = video.seekable.end(video.seekable.length - 1);
            const target = Math.max(0, edge - 3);
            const latency = edge - video.currentTime;

            if (force || latency > 12) {
                video.currentTime = target;
            }
        };

        const handleLoadedMetadata = () => seekToLiveEdge(true);
        const handleCanPlay = () => seekToLiveEdge(true);
        const handleTimeUpdate = () => seekToLiveEdge(false);

        video.addEventListener('loadedmetadata', handleLoadedMetadata);
        video.addEventListener('canplay', handleCanPlay, { once: true });
        video.addEventListener('play', handleCanPlay);
        video.addEventListener('timeupdate', handleTimeUpdate);

        if (video.canPlayType('application/vnd.apple.mpegurl')) {
            video.src = src;

            return () => {
                video.removeEventListener(
                    'loadedmetadata',
                    handleLoadedMetadata,
                );
                video.removeEventListener('canplay', handleCanPlay);
                video.removeEventListener('play', handleCanPlay);
                video.removeEventListener('timeupdate', handleTimeUpdate);
            };
        }

        const loadHls = async () => {
            if (!window.Hls) {
                await new Promise<void>((resolve, reject) => {
                    const existing = document.querySelector(
                        `script[src="${HLS_SCRIPT_SRC}"]`,
                    );

                    if (existing) {
                        existing.addEventListener('load', () => resolve(), {
                            once: true,
                        });
                        existing.addEventListener('error', () => reject(), {
                            once: true,
                        });

                        return;
                    }

                    const script = document.createElement('script');
                    script.src = HLS_SCRIPT_SRC;
                    script.async = true;
                    script.onload = () => resolve();
                    script.onerror = () => reject();
                    document.head.appendChild(script);
                });
            }

            if (cancelled || !window.Hls?.isSupported()) {
                return;
            }

            hls = new window.Hls({
                lowLatencyMode: true,
                liveSyncDuration: 3,
                liveMaxLatencyDuration: 10,
                liveDurationInfinity: true,
                maxLiveSyncPlaybackRate: 1.5,
            });

            hls.on(window.Hls.Events.MANIFEST_PARSED, () => {
                seekToLiveEdge(true);
            });
            hls.loadSource(src);
            hls.attachMedia(video);
        };

        void loadHls().catch(() => undefined);

        return () => {
            cancelled = true;
            video.removeEventListener('loadedmetadata', handleLoadedMetadata);
            video.removeEventListener('canplay', handleCanPlay);
            video.removeEventListener('play', handleCanPlay);
            video.removeEventListener('timeupdate', handleTimeUpdate);
            hls?.destroy();
        };
    }, [src]);

    return (
        <div className="relative aspect-video overflow-hidden rounded-lg border bg-black">
            <video
                ref={videoRef}
                controls
                playsInline
                className="h-full w-full"
            />
            {!isLive && (
                <div className="absolute inset-0 flex items-center justify-center bg-black/70 text-sm text-white">
                    Stream is not live
                </div>
            )}
        </div>
    );
}

function ViewerChart({ rollups }: { rollups: ViewerRollup[] }) {
    const max = Math.max(1, ...rollups.map((rollup) => rollup.current_viewers));

    if (rollups.length === 0) {
        return (
            <div className="flex h-28 items-center justify-center rounded-lg border border-dashed text-xs text-muted-foreground">
                No viewer samples yet.
            </div>
        );
    }

    return (
        <div className="flex h-28 items-end gap-1 rounded-lg border p-2">
            {rollups.map((rollup, index) => (
                <div
                    key={rollup.minute ?? index}
                    className="flex min-w-2 flex-1 items-end"
                    title={`${rollup.current_viewers} viewers`}
                >
                    <div
                        className="w-full rounded-sm bg-primary/70"
                        style={{
                            height: `${Math.max(
                                6,
                                (rollup.current_viewers / max) * 100,
                            )}%`,
                        }}
                    />
                </div>
            ))}
        </div>
    );
}

export default function ShowLiveStream({ organization, liveStream }: Props) {
    const [recordingEnabled, setRecordingEnabled] = useState(
        liveStream.recording_enabled,
    );

    usePoll(
        5000,
        {
            only: ['liveStream'],
            // Don't re-send the long-lived publishing credentials on every 5s
            // tick; they're delivered once on the full page load and never
            // change during polling.
            except: [
                'liveStream.stream_key',
                'liveStream.rtmp_url',
                'liveStream.stream_path',
            ],
        },
        {
            autoStart:
                liveStream.status === 'live' ||
                liveStream.status === 'restarting',
            keepAlive: false,
        },
    );

    setLayoutProps({
        breadcrumbs: [
            {
                title: 'Live Streams',
                href: indexOrgLiveStreams({ organization: organization.id }),
            },
            {
                title: liveStream.title,
                href: showOrgLiveStream({
                    organization: organization.id,
                    liveStream: liveStream.id,
                }),
            },
        ],
    });

    const publishName = `${liveStream.stream_path}?key=${liveStream.stream_key}`;
    const session = liveStream.current_session;

    const stats = useMemo(
        () => [
            {
                label: 'Current viewers',
                value: session?.current_viewers ?? 0,
                icon: Eye,
            },
            {
                label: 'Peak viewers',
                value: session?.peak_viewers ?? 0,
                icon: Radio,
            },
            {
                label: 'Unique viewers',
                value: session?.unique_viewers ?? 0,
                icon: Video,
            },
        ],
        [session],
    );

    return (
        <>
            <Head title={liveStream.title} />

            <h1 className="sr-only">{liveStream.title}</h1>

            <div className="flex h-full flex-1 flex-col gap-4 overflow-x-auto rounded-xl p-4">
                <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                    <Heading
                        variant="page"
                        title={liveStream.title}
                        description="RTMP ingest credentials, live playback, and stream reporting."
                    />
                    <div className="flex flex-wrap gap-2">
                        <Badge className={statusClasses[liveStream.status]}>
                            {liveStream.status_label}
                        </Badge>
                        {liveStream.recording_enabled && (
                            <Badge variant="secondary">Recording on</Badge>
                        )}
                        {liveStream.restart_required && (
                            <Badge
                                variant="outline"
                                className="gap-1 text-amber-600"
                            >
                                <RotateCcw className="size-3" />
                                Restart required
                            </Badge>
                        )}
                    </div>
                </div>

                {liveStream.restart_required && (
                    <Alert>
                        <RotateCcw className="size-3.5" />
                        <AlertTitle>Restart required</AlertTitle>
                        <AlertDescription>
                            Current settings changed while the stream was live.
                            Restart the stream to apply them.
                        </AlertDescription>
                    </Alert>
                )}

                <div className="grid gap-4 xl:grid-cols-[minmax(0,1.5fr)_minmax(360px,0.85fr)]">
                    <div className="grid gap-4">
                        <LivePlayer
                            src={liveStream.hls_url}
                            isLive={liveStream.status === 'live'}
                        />

                        <div className="grid gap-3 sm:grid-cols-3">
                            {stats.map((stat) => {
                                const Icon = stat.icon;

                                return (
                                    <Card key={stat.label}>
                                        <CardHeader>
                                            <CardTitle className="flex items-center gap-2">
                                                <Icon className="size-3.5 text-muted-foreground" />
                                                {stat.label}
                                            </CardTitle>
                                        </CardHeader>
                                        <CardContent>
                                            <div className="text-2xl font-semibold tabular-nums">
                                                {stat.value}
                                            </div>
                                        </CardContent>
                                    </Card>
                                );
                            })}
                        </div>

                        <Card>
                            <CardHeader>
                                <CardTitle>Viewer history</CardTitle>
                            </CardHeader>
                            <CardContent>
                                <ViewerChart
                                    rollups={liveStream.viewer_rollups}
                                />
                            </CardContent>
                        </Card>
                    </div>

                    <div className="grid content-start gap-4">
                        <Card>
                            <CardHeader>
                                <CardTitle className="flex items-center gap-2">
                                    <KeyRound className="size-3.5" />
                                    Credentials
                                </CardTitle>
                            </CardHeader>
                            <CardContent className="grid gap-4">
                                <CopyField
                                    label="Server"
                                    value={liveStream.rtmp_url}
                                />
                                <CopyField
                                    label="Stream key"
                                    value={publishName}
                                    secret
                                />
                            </CardContent>
                        </Card>

                        <Card>
                            <CardHeader>
                                <CardTitle>Playback</CardTitle>
                            </CardHeader>
                            <CardContent>
                                <CopyField
                                    label="M3U8 URL"
                                    value={liveStream.hls_url}
                                />
                            </CardContent>
                        </Card>

                        <Card>
                            <CardHeader>
                                <CardTitle className="flex items-center gap-2">
                                    <Settings className="size-3.5" />
                                    Settings
                                </CardTitle>
                            </CardHeader>
                            <CardContent>
                                <Form
                                    {...OrganizationLiveStreamsController.update.form(
                                        {
                                            organization: organization.id,
                                            liveStream: liveStream.id,
                                        },
                                    )}
                                    options={{ preserveScroll: true }}
                                    className="grid gap-4"
                                >
                                    {({ processing, errors }) => (
                                        <>
                                            <div className="grid gap-2">
                                                <Label htmlFor="title">
                                                    Title
                                                </Label>
                                                <Input
                                                    id="title"
                                                    name="title"
                                                    defaultValue={
                                                        liveStream.title
                                                    }
                                                    required
                                                />
                                                <InputError
                                                    message={errors.title}
                                                />
                                            </div>

                                            <input
                                                type="hidden"
                                                name="recording_enabled"
                                                value={
                                                    recordingEnabled ? '1' : '0'
                                                }
                                            />
                                            <div className="flex items-start gap-3 rounded-lg border p-3">
                                                <Checkbox
                                                    id="recording_enabled"
                                                    checked={recordingEnabled}
                                                    onCheckedChange={(
                                                        checked,
                                                    ) =>
                                                        setRecordingEnabled(
                                                            checked === true,
                                                        )
                                                    }
                                                />
                                                <div className="grid gap-1">
                                                    <Label htmlFor="recording_enabled">
                                                        Record this stream
                                                    </Label>
                                                    <p className="text-xs text-muted-foreground">
                                                        Changing this while live
                                                        requires a restart.
                                                    </p>
                                                </div>
                                            </div>

                                            <Button disabled={processing}>
                                                Save settings
                                            </Button>
                                        </>
                                    )}
                                </Form>
                            </CardContent>
                        </Card>

                        <div className="grid gap-2">
                            <Form
                                {...OrganizationLiveStreamsController.rotateKey.form(
                                    {
                                        organization: organization.id,
                                        liveStream: liveStream.id,
                                    },
                                )}
                                options={{ preserveScroll: true }}
                            >
                                {({ processing }) => (
                                    <Button
                                        variant="outline"
                                        disabled={processing}
                                        className="w-full"
                                    >
                                        <KeyRound className="size-3.5" />
                                        Rotate stream key
                                    </Button>
                                )}
                            </Form>

                            <Form
                                {...OrganizationLiveStreamsController.restart.form(
                                    {
                                        organization: organization.id,
                                        liveStream: liveStream.id,
                                    },
                                )}
                                options={{ preserveScroll: true }}
                            >
                                {({ processing }) => (
                                    <Button
                                        variant="outline"
                                        disabled={
                                            processing ||
                                            liveStream.status !== 'live'
                                        }
                                        className="w-full"
                                    >
                                        <RotateCcw className="size-3.5" />
                                        Restart stream
                                    </Button>
                                )}
                            </Form>

                            <Form
                                {...OrganizationLiveStreamsController.disable.form(
                                    {
                                        organization: organization.id,
                                        liveStream: liveStream.id,
                                    },
                                )}
                                options={{ preserveScroll: true }}
                            >
                                {({ processing }) => (
                                    <Button
                                        variant="destructive"
                                        disabled={processing}
                                        className="w-full"
                                    >
                                        <ShieldOff className="size-3.5" />
                                        Disable stream
                                    </Button>
                                )}
                            </Form>
                        </div>
                    </div>
                </div>

                <div className="rounded-lg border">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead>Started</TableHead>
                                <TableHead>Ended</TableHead>
                                <TableHead>Status</TableHead>
                                <TableHead>Recording</TableHead>
                                <TableHead>Peak</TableHead>
                                <TableHead>Unique</TableHead>
                                <TableHead>Recording path</TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {liveStream.recent_sessions.map((item) => (
                                <TableRow key={item.id}>
                                    <TableCell>
                                        {formatDate(item.started_at)}
                                    </TableCell>
                                    <TableCell>
                                        {formatDate(item.ended_at)}
                                    </TableCell>
                                    <TableCell>
                                        <Badge variant="outline">
                                            {item.status}
                                        </Badge>
                                    </TableCell>
                                    <TableCell>
                                        {item.recording_enabled ? 'On' : 'Off'}
                                    </TableCell>
                                    <TableCell>{item.peak_viewers}</TableCell>
                                    <TableCell>{item.unique_viewers}</TableCell>
                                    <TableCell className="max-w-xs truncate font-mono text-xs text-muted-foreground">
                                        {item.recording_path ?? '-'}
                                    </TableCell>
                                </TableRow>
                            ))}

                            {liveStream.recent_sessions.length === 0 && (
                                <TableRow>
                                    <TableCell
                                        colSpan={7}
                                        className="h-24 text-center text-muted-foreground"
                                    >
                                        No sessions yet.
                                    </TableCell>
                                </TableRow>
                            )}
                        </TableBody>
                    </Table>
                </div>

                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                    <LinkIcon className="size-3.5" />
                    <span>Settings version {liveStream.settings_version}</span>
                </div>
            </div>
        </>
    );
}
