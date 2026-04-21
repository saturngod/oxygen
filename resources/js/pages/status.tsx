import { Head, usePoll } from '@inertiajs/react';
import {
    AlertCircle,
    CheckCircle2,
    Clock,
    Copy,
    Loader2,
    Video,
} from 'lucide-react';
import { useState } from 'react';
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
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table';

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

    if (file.status === 'failed') {
        return (
            <span className="text-xs text-red-600 dark:text-red-400">
                Failed
            </span>
        );
    }

    return <span className="text-xs text-muted-foreground">Pending</span>;
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
                    <div className="flex flex-1 flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-sidebar-border/70 p-12 text-center">
                        <Video className="size-10 text-muted-foreground/50" />
                        <p className="text-sm text-muted-foreground">
                            No videos yet. Upload a video to see its transcode
                            progress here.
                        </p>
                    </div>
                ) : (
                    <div className="relative flex-1 overflow-hidden rounded-xl border border-sidebar-border/70 dark:border-sidebar-border">
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
                <DialogContent className="sm:max-w-lg">
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
                            <div className="grid grid-cols-[8rem_1fr] gap-x-4 gap-y-2">
                                <span className="text-xs text-muted-foreground">
                                    Status
                                </span>
                                <div className="space-y-1.5">
                                    <Badge
                                        variant="secondary"
                                        className={
                                            statusStyles[fileDetails.status]
                                        }
                                    >
                                        {statusLabel[fileDetails.status]}
                                        {fileDetails.status === 'progress' &&
                                            ` · ${fileDetails.progress}%`}
                                    </Badge>
                                    {fileDetails.status === 'progress' && (
                                        <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
                                            <div
                                                className="h-full bg-primary transition-all"
                                                style={{
                                                    width: `${fileDetails.progress}%`,
                                                }}
                                            />
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
                                            <Badge
                                                key={tag}
                                                variant="outline"
                                            >
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
                                                href={
                                                    fileDetails.streaming_url
                                                }
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
                                        {fileDetails.profiles.map(
                                            (profile) => (
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
                                                                profile
                                                                    .qualities
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
                                                                    key={
                                                                        quality
                                                                    }
                                                                    variant="secondary"
                                                                >
                                                                    {quality}
                                                                </Badge>
                                                            ),
                                                        )}
                                                    </div>
                                                </div>
                                            ),
                                        )}
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
