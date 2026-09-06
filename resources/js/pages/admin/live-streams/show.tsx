import {
    Form,
    Head,
    Link,
    router,
    setLayoutProps,
    usePoll,
} from '@inertiajs/react';
import {
    ChartNoAxesCombined,
    Copy,
    Eye,
    KeyRound,
    Link as LinkIcon,
    Power,
    Radio,
    RotateCcw,
    Settings,
    ShieldOff,
    Trash2,
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
import {
    index as indexOrgLiveStreams,
    show as showOrgLiveStream,
    viewer as viewOrgLiveStreamAnalytics,
} from '@/routes/admin/organizations/live-streams';

type AdminHlsErrorData = {
    details?: string;
    fatal?: boolean;
    type?: string;
};

type AdminHlsInstance = {
    attachMedia: (media: HTMLMediaElement) => void;
    destroy: () => void;
    loadSource: (src: string) => void;
    recoverMediaError: () => void;
    startLoad: () => void;
    on: (
        event: string,
        callback: (event: string, data: AdminHlsErrorData) => void,
    ) => void;
};

type AdminHlsConstructor = {
    Events: {
        ERROR: string;
        MANIFEST_PARSED: string;
    };
    ErrorTypes: {
        MEDIA_ERROR: string;
        NETWORK_ERROR: string;
    };
    isSupported: () => boolean;
    new (config?: Record<string, unknown>): AdminHlsInstance;
};

function getHlsConstructor(): AdminHlsConstructor | undefined {
    return (window as Window & { Hls?: AdminHlsConstructor }).Hls;
}

const HLS_SCRIPT_SRC =
    'https://cdn.jsdelivr.net/npm/hls.js@1.6.16/dist/hls.min.js';
const PLAYER_RETRY_LIMIT_MS = 30_000;
const PLAYER_RETRY_DELAYS_MS = [1_000, 2_000, 4_000] as const;
let hlsScriptPromise: Promise<void> | null = null;

function loadHlsScript(): Promise<void> {
    if (getHlsConstructor()) {
        return Promise.resolve();
    }

    if (hlsScriptPromise) {
        return hlsScriptPromise;
    }

    hlsScriptPromise = new Promise<void>((resolve, reject) => {
        let script = document.querySelector<HTMLScriptElement>(
            `script[src="${HLS_SCRIPT_SRC}"]`,
        );

        if (script && script.dataset.oxygenHlsLoading !== 'true') {
            script.remove();
            script = null;
        }

        const shouldAppend = !script;

        if (shouldAppend) {
            script = document.createElement('script');
            script.src = HLS_SCRIPT_SRC;
            script.async = true;
            script.dataset.oxygenHlsLoading = 'true';
        }

        if (!script) {
            reject(new Error('Could not create the HLS player script'));

            return;
        }

        const target = script;
        const timeout = window.setTimeout(() => {
            target.removeEventListener('load', loaded);
            target.removeEventListener('error', failed);
            reject(new Error('Timed out loading the HLS player'));
        }, 10_000);
        const loaded = () => {
            window.clearTimeout(timeout);
            target.dataset.oxygenHlsLoading = 'false';

            if (getHlsConstructor()) {
                resolve();

                return;
            }

            reject(new Error('The HLS player script did not initialize'));
        };
        const failed = () => {
            window.clearTimeout(timeout);
            target.dataset.oxygenHlsLoading = 'false';
            reject(new Error('Could not load the HLS player'));
        };
        target.addEventListener('load', loaded, { once: true });
        target.addEventListener('error', failed, { once: true });

        if (shouldAppend) {
            document.head.appendChild(target);
        }
    }).catch((error: unknown) => {
        hlsScriptPromise = null;

        throw error;
    });

    return hlsScriptPromise;
}

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
    hls_url: string | null;
    current_viewers: number;
    peak_viewers: number;
    unique_viewers: number;
    playlist_requests: number;
    segment_requests: number;
    started_at: string | null;
    ended_at: string | null;
    error_message: string | null;
};

type LiveStream = {
    id: string;
    title: string;
    public_id: string;
    stream_key: string;
    stream_path: string;
    status: LiveStreamStatus;
    status_label: string;
    restart_required: boolean;
    settings_version: number;
    rtmp_url: string;
    hls_url: string;
    last_started_at: string | null;
    last_ended_at: string | null;
    current_session: StreamSession | null;
    recent_sessions: StreamSession[];
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

function LivePlayer({
    src,
    isLive,
    sessionId,
}: {
    src: string;
    isLive: boolean;
    sessionId: string | null;
}) {
    const videoRef = useRef<HTMLVideoElement | null>(null);
    const [playbackError, setPlaybackError] = useState<{
        sessionId: string | null;
        message: string;
    } | null>(null);

    useEffect(() => {
        const video = videoRef.current;
        let cancelled = false;
        let hls: AdminHlsInstance | null = null;
        let mediaRecoveryUsed = false;
        let retryIndex = 0;
        let retryStartedAt = Date.now();
        const timers = new Set<number>();

        if (!video || !src) {
            return;
        }

        const clearMedia = () => {
            video.pause();
            video.removeAttribute('src');
            video.load();
        };

        if (!isLive) {
            clearMedia();

            return;
        }

        const showFailure = (message: string) => {
            if (!cancelled) {
                setPlaybackError({ sessionId, message });
            }
        };
        const scheduleRetry = (retry: () => void, details?: string) => {
            if (timers.size > 0) {
                return;
            }

            const delay =
                PLAYER_RETRY_DELAYS_MS[
                    Math.min(retryIndex, PLAYER_RETRY_DELAYS_MS.length - 1)
                ];
            retryIndex += 1;

            if (Date.now() - retryStartedAt + delay > PLAYER_RETRY_LIMIT_MS) {
                showFailure(
                    `Playback could not reconnect within 30 seconds${details ? ` (${details})` : ''}. Check the publisher and HLS proxy, then retry.`,
                );

                return;
            }

            const timer = window.setTimeout(() => {
                timers.delete(timer);

                if (!cancelled) {
                    retry();
                }
            }, delay);
            timers.add(timer);
        };
        const handlePlayable = () => {
            retryIndex = 0;
            retryStartedAt = Date.now();
            setPlaybackError((current) =>
                current?.sessionId === sessionId ? null : current,
            );
        };
        video.addEventListener('canplay', handlePlayable);

        if (video.canPlayType('application/vnd.apple.mpegurl')) {
            const loadNative = () => {
                clearMedia();
                video.src = src;
                video.load();
            };
            const handleNativeError = () => {
                scheduleRetry(loadNative, 'native HLS network error');
            };
            video.addEventListener('error', handleNativeError);
            loadNative();

            return () => {
                cancelled = true;
                timers.forEach((timer) => window.clearTimeout(timer));
                video.removeEventListener('canplay', handlePlayable);
                video.removeEventListener('error', handleNativeError);
                clearMedia();
            };
        }

        const loadHls = async () => {
            await loadHlsScript();

            const Hls = getHlsConstructor();

            if (cancelled) {
                return;
            }

            if (!Hls?.isSupported()) {
                showFailure('This browser does not support HLS playback.');

                return;
            }

            const instance = new Hls({
                lowLatencyMode: false,
                liveSyncDurationCount: 2,
                liveMaxLatencyDurationCount: 4,
                liveDurationInfinity: true,
                maxLiveSyncPlaybackRate: 1.5,
            });

            hls = instance;
            instance.on(Hls.Events.MANIFEST_PARSED, () => {
                handlePlayable();
            });
            instance.on(Hls.Events.ERROR, (_event, data) => {
                if (!data.fatal || !hls) {
                    return;
                }

                if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
                    scheduleRetry(() => {
                        hls?.loadSource(src);
                        hls?.startLoad();
                    }, data.details);

                    return;
                }

                if (
                    data.type === Hls.ErrorTypes.MEDIA_ERROR &&
                    !mediaRecoveryUsed
                ) {
                    mediaRecoveryUsed = true;
                    instance.recoverMediaError();

                    return;
                }

                showFailure(
                    `Playback stopped${data.details ? ` (${data.details})` : ''}. Check the incoming stream codec and HLS output.`,
                );
            });
            instance.attachMedia(video);
            instance.loadSource(src);
        };

        void loadHls().catch((error: unknown) => {
            showFailure(
                error instanceof Error
                    ? error.message
                    : 'Could not initialize HLS playback',
            );
        });

        return () => {
            cancelled = true;
            timers.forEach((timer) => window.clearTimeout(timer));
            video.removeEventListener('canplay', handlePlayable);
            hls?.destroy();
            clearMedia();
        };
    }, [src, isLive, sessionId]);

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
            {isLive && playbackError?.sessionId === sessionId && (
                <div className="absolute inset-x-3 bottom-3 rounded-md border border-red-400/40 bg-red-950/90 p-3 text-sm text-red-50 shadow-lg">
                    {playbackError.message}
                </div>
            )}
        </div>
    );
}

export default function ShowLiveStream({ organization, liveStream }: Props) {
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
            // Keep polling while the detail page is open so enabling a stream
            // immediately resumes lifecycle and viewer updates.
            autoStart: true,
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
    const [confirmingDelete, setConfirmingDelete] = useState(false);
    const [deleting, setDeleting] = useState(false);

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
                    <div className="flex flex-wrap items-center gap-2">
                        <Button asChild variant="outline">
                            <Link
                                href={
                                    viewOrgLiveStreamAnalytics({
                                        organization: organization.id,
                                        liveStream: liveStream.id,
                                    }).url
                                }
                            >
                                <ChartNoAxesCombined data-icon="inline-start" />
                                Viewer analytics
                            </Link>
                        </Button>
                        <Badge className={statusClasses[liveStream.status]}>
                            {liveStream.status_label}
                        </Badge>
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
                            sessionId={liveStream.current_session?.id ?? null}
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

                            {liveStream.status === 'disabled' ? (
                                <Form
                                    {...OrganizationLiveStreamsController.enable.form(
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
                                            <Power data-icon="inline-start" />
                                            Enable stream
                                        </Button>
                                    )}
                                </Form>
                            ) : (
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
                                            <ShieldOff data-icon="inline-start" />
                                            Disable stream
                                        </Button>
                                    )}
                                </Form>
                            )}

                            <Button
                                variant="destructive"
                                className="w-full"
                                onClick={() => setConfirmingDelete(true)}
                            >
                                <Trash2 className="size-3.5" />
                                Delete stream
                            </Button>
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
                                <TableHead>Peak</TableHead>
                                <TableHead>Unique</TableHead>
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
                                    <TableCell>{item.peak_viewers}</TableCell>
                                    <TableCell>{item.unique_viewers}</TableCell>
                                </TableRow>
                            ))}

                            {liveStream.recent_sessions.length === 0 && (
                                <TableRow>
                                    <TableCell
                                        colSpan={5}
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

            <Dialog
                open={confirmingDelete}
                onOpenChange={(open) => {
                    if (!open) {
                        setConfirmingDelete(false);
                        setDeleting(false);
                    }
                }}
            >
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Delete live stream?</DialogTitle>
                        <DialogDescription>
                            Permanently delete{' '}
                            <span className="font-medium text-foreground">
                                {liveStream.title}
                            </span>{' '}
                            along with its session history and viewer analytics.
                            Any active publisher will be disconnected. This
                            action cannot be undone.
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <DialogClose asChild>
                            <Button variant="outline" disabled={deleting}>
                                Cancel
                            </Button>
                        </DialogClose>
                        <Button
                            variant="destructive"
                            disabled={deleting}
                            onClick={() => {
                                setDeleting(true);
                                router.delete(
                                    OrganizationLiveStreamsController.destroy.url(
                                        {
                                            organization: organization.id,
                                            liveStream: liveStream.id,
                                        },
                                    ),
                                    {
                                        onFinish: () => {
                                            setDeleting(false);
                                            setConfirmingDelete(false);
                                        },
                                    },
                                );
                            }}
                        >
                            Delete stream
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </>
    );
}
