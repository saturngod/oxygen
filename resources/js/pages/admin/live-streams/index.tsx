import { Head, router, setLayoutProps } from '@inertiajs/react';
import {
    AlertTriangle,
    Eye,
    Plus,
    Radio,
    RotateCcw,
    Video,
} from 'lucide-react';
import { useState } from 'react';
import { ControlFilter } from '@/components/control-filter';
import Heading from '@/components/heading';
import { Badge } from '@/components/ui/badge';
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table';
import {
    create as createOrgLiveStream,
    index as indexOrgLiveStreams,
    show as showOrgLiveStream,
} from '@/routes/admin/organizations/live-streams';

type LiveStreamStatus =
    | 'idle'
    | 'live'
    | 'offline'
    | 'restarting'
    | 'failed'
    | 'disabled';

type LiveStream = {
    id: string;
    title: string;
    public_id: string;
    status: LiveStreamStatus;
    status_label: string;
    recording_enabled: boolean;
    restart_required: boolean;
    current_viewers: number;
    peak_viewers: number;
    created_at: string | null;
    last_started_at: string | null;
};

type Props = {
    organization: {
        id: string;
        name: string;
    };
    liveStreams: LiveStream[];
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

export default function LiveStreamsIndex({ organization, liveStreams }: Props) {
    const [search, setSearch] = useState('');

    setLayoutProps({
        breadcrumbs: [
            {
                title: 'Live Streams',
                href: indexOrgLiveStreams({ organization: organization.id }),
            },
        ],
    });

    const filtered = liveStreams.filter((stream) =>
        stream.title.toLowerCase().includes(search.toLowerCase()),
    );

    return (
        <>
            <Head title="Live Streams" />

            <h1 className="sr-only">Live Streams</h1>

            <div className="flex h-full flex-1 flex-col gap-4 overflow-x-auto rounded-xl p-4">
                <Heading
                    variant="page"
                    title="Live Streams"
                    description={`Create and monitor live streams for ${organization.name}`}
                />

                <ControlFilter
                    searchValue={search}
                    onSearchChange={setSearch}
                    searchPlaceholder="Search streams..."
                    actions={[
                        {
                            label: 'Add Stream',
                            icon: <Plus className="size-3.5" />,
                            onClick: () =>
                                router.visit(
                                    createOrgLiveStream({
                                        organization: organization.id,
                                    }).url,
                                ),
                        },
                    ]}
                />

                <div className="rounded-lg border">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead>Stream</TableHead>
                                <TableHead>Status</TableHead>
                                <TableHead>Recording</TableHead>
                                <TableHead>Viewers</TableHead>
                                <TableHead>Last live</TableHead>
                                <TableHead>Created</TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {filtered.map((stream) => (
                                <TableRow
                                    key={stream.id}
                                    className="cursor-pointer"
                                    onClick={() =>
                                        router.visit(
                                            showOrgLiveStream({
                                                organization: organization.id,
                                                liveStream: stream.id,
                                            }).url,
                                        )
                                    }
                                >
                                    <TableCell>
                                        <div className="flex flex-col gap-1">
                                            <div className="flex items-center gap-2 font-medium">
                                                <Radio className="size-3.5 text-muted-foreground" />
                                                {stream.title}
                                            </div>
                                            <div className="font-mono text-xs text-muted-foreground">
                                                {stream.public_id}
                                            </div>
                                        </div>
                                    </TableCell>
                                    <TableCell>
                                        <div className="flex flex-wrap gap-1">
                                            <Badge
                                                className={
                                                    statusClasses[stream.status]
                                                }
                                            >
                                                {stream.status_label}
                                            </Badge>
                                            {stream.restart_required && (
                                                <Badge
                                                    variant="outline"
                                                    className="gap-1 text-amber-600"
                                                >
                                                    <RotateCcw className="size-3" />
                                                    Restart
                                                </Badge>
                                            )}
                                        </div>
                                    </TableCell>
                                    <TableCell>
                                        {stream.recording_enabled ? (
                                            <Badge variant="secondary">
                                                On
                                            </Badge>
                                        ) : (
                                            <span className="text-xs text-muted-foreground">
                                                Off
                                            </span>
                                        )}
                                    </TableCell>
                                    <TableCell>
                                        <div className="flex items-center gap-1.5 text-sm">
                                            <Eye className="size-3.5 text-muted-foreground" />
                                            <span>
                                                {stream.current_viewers}
                                            </span>
                                            <span className="text-xs text-muted-foreground">
                                                peak {stream.peak_viewers}
                                            </span>
                                        </div>
                                    </TableCell>
                                    <TableCell className="text-muted-foreground">
                                        {formatDate(stream.last_started_at)}
                                    </TableCell>
                                    <TableCell className="text-muted-foreground">
                                        {formatDate(stream.created_at)}
                                    </TableCell>
                                </TableRow>
                            ))}

                            {filtered.length === 0 && (
                                <TableRow>
                                    <TableCell
                                        colSpan={6}
                                        className="h-32 text-center"
                                    >
                                        <div className="flex flex-col items-center gap-2 text-muted-foreground">
                                            {liveStreams.length === 0 ? (
                                                <Video className="size-9 opacity-60" />
                                            ) : (
                                                <AlertTriangle className="size-9 opacity-60" />
                                            )}
                                            <span>No live streams found.</span>
                                        </div>
                                    </TableCell>
                                </TableRow>
                            )}
                        </TableBody>
                    </Table>
                </div>
            </div>
        </>
    );
}
