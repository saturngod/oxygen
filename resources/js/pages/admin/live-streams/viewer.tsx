import { Head, Link, router, setLayoutProps } from '@inertiajs/react';
import {
    ArrowLeft,
    Eye,
    ListVideo,
    MousePointerClick,
    Users,
} from 'lucide-react';
import Heading from '@/components/heading';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
    Card,
    CardContent,
    CardDescription,
    CardHeader,
    CardTitle,
} from '@/components/ui/card';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import {
    Tooltip,
    TooltipContent,
    TooltipTrigger,
} from '@/components/ui/tooltip';
import {
    index as indexOrgLiveStreams,
    show as showOrgLiveStream,
    viewer as viewOrgLiveStreamAnalytics,
} from '@/routes/admin/organizations/live-streams';

type Period = 'day' | 'month' | 'year';

type AnalyticsPoint = {
    timestamp: string;
    value: number;
};

type Analytics = {
    range_label: string;
    range_start: string;
    range_end: string;
    timezone: 'UTC';
    granularity: 'hour' | 'day' | 'month';
    source_note: string;
    points: AnalyticsPoint[];
    summary: {
        peak_viewers: number;
        broadcasts: number;
        viewer_identity_additions: number;
        playback_requests: number;
    };
};

type Props = {
    organization: {
        id: string;
        name: string;
    };
    liveStream: {
        id: string;
        title: string;
        status: string;
        status_label: string;
    };
    period: Period;
    analytics: Analytics;
};

const periods: Array<{ value: Period; label: string }> = [
    { value: 'day', label: 'Day' },
    { value: 'month', label: 'Month' },
    { value: 'year', label: 'Year' },
];

function formatPointLabel(
    timestamp: string,
    period: Period,
    detailed = false,
): string {
    const date = new Date(timestamp);

    if (period === 'day') {
        return date.toLocaleString(undefined, {
            month: detailed ? 'short' : undefined,
            day: detailed ? 'numeric' : undefined,
            hour: 'numeric',
            timeZone: 'UTC',
            timeZoneName: detailed ? 'short' : undefined,
        });
    }

    if (period === 'month') {
        return date.toLocaleDateString(undefined, {
            month: 'short',
            day: 'numeric',
            year: detailed ? 'numeric' : undefined,
            timeZone: 'UTC',
        });
    }

    return date.toLocaleDateString(undefined, {
        month: 'short',
        year: detailed ? 'numeric' : undefined,
        timeZone: 'UTC',
    });
}

function axisLabelIndexes(period: Period): number[] {
    return period === 'day'
        ? [0, 6, 12, 18, 23]
        : period === 'month'
          ? [0, 7, 14, 21, 29]
          : [0, 2, 5, 8, 11];
}

function ViewerLineChart({
    points,
    period,
}: {
    points: AnalyticsPoint[];
    period: Period;
}) {
    const width = 960;
    const height = 320;
    const padding = { top: 20, right: 24, bottom: 48, left: 56 };
    const plotWidth = width - padding.left - padding.right;
    const plotHeight = height - padding.top - padding.bottom;
    const maximum = Math.max(0, ...points.map((point) => point.value));
    const axisMaximum = Math.max(4, Math.ceil(maximum / 4) * 4);
    const coordinates = points.map((point, index) => ({
        ...point,
        x: padding.left + (index / Math.max(1, points.length - 1)) * plotWidth,
        y: padding.top + plotHeight - (point.value / axisMaximum) * plotHeight,
    }));
    const line = coordinates.map((point) => `${point.x},${point.y}`).join(' ');
    const labels = axisLabelIndexes(period);

    return (
        <div className="flex flex-col gap-3">
            <div className="overflow-x-auto">
                <svg
                    viewBox={`0 0 ${width} ${height}`}
                    className="h-auto w-full min-w-[680px] overflow-visible"
                    role="img"
                    aria-labelledby="viewer-chart-title viewer-chart-description"
                >
                    <title id="viewer-chart-title">
                        Peak concurrent viewers over {points.length}{' '}
                        {period === 'day'
                            ? 'hours'
                            : period === 'month'
                              ? 'days'
                              : 'months'}
                    </title>
                    <desc id="viewer-chart-description">
                        A line chart where each point is the highest concurrent
                        viewer count recorded in that time bucket.
                    </desc>

                    {[0, 1, 2, 3, 4].map((tick) => {
                        const value = (axisMaximum / 4) * tick;
                        const y =
                            padding.top + plotHeight - (tick / 4) * plotHeight;

                        return (
                            <g key={tick}>
                                <line
                                    x1={padding.left}
                                    x2={width - padding.right}
                                    y1={y}
                                    y2={y}
                                    className="text-border"
                                    stroke="currentColor"
                                    strokeWidth="1"
                                />
                                <text
                                    x={padding.left - 12}
                                    y={y + 4}
                                    textAnchor="end"
                                    className="fill-muted-foreground text-[11px] tabular-nums"
                                >
                                    {value}
                                </text>
                            </g>
                        );
                    })}

                    {labels.map((index) => {
                        const point = coordinates[index];

                        if (!point) {
                            return null;
                        }

                        return (
                            <text
                                key={point.timestamp}
                                x={point.x}
                                y={height - 14}
                                textAnchor={
                                    index === 0
                                        ? 'start'
                                        : index === points.length - 1
                                          ? 'end'
                                          : 'middle'
                                }
                                className="fill-muted-foreground text-[11px]"
                            >
                                {formatPointLabel(point.timestamp, period)}
                            </text>
                        );
                    })}

                    <polyline
                        points={line}
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="3"
                        strokeLinejoin="round"
                        strokeLinecap="round"
                        className="text-primary"
                    />

                    {coordinates.map((point) => (
                        <Tooltip key={point.timestamp}>
                            <TooltipTrigger asChild>
                                <g
                                    tabIndex={0}
                                    aria-label={`${formatPointLabel(point.timestamp, period, true)}: ${point.value} concurrent viewers`}
                                    className="group/chart-point cursor-crosshair outline-none"
                                >
                                    <circle
                                        cx={point.x}
                                        cy={point.y}
                                        r="12"
                                        className="fill-transparent"
                                    />
                                    <circle
                                        cx={point.x}
                                        cy={point.y}
                                        r="4"
                                        fill="currentColor"
                                        stroke="var(--background)"
                                        strokeWidth="2"
                                        className="pointer-events-none text-primary group-focus-visible/chart-point:stroke-ring group-focus-visible/chart-point:[stroke-width:4px]"
                                    />
                                </g>
                            </TooltipTrigger>
                            <TooltipContent side="top" sideOffset={8}>
                                <div className="flex flex-col gap-0.5">
                                    <span className="font-medium">
                                        {point.value.toLocaleString()}{' '}
                                        {point.value === 1
                                            ? 'viewer'
                                            : 'viewers'}
                                    </span>
                                    <span className="opacity-75">
                                        {formatPointLabel(
                                            point.timestamp,
                                            period,
                                            true,
                                        )}
                                    </span>
                                </div>
                            </TooltipContent>
                        </Tooltip>
                    ))}
                </svg>
            </div>
            {maximum === 0 && (
                <p className="text-center text-muted-foreground">
                    No viewer activity was recorded in this range.
                </p>
            )}
        </div>
    );
}

export default function ViewerAnalytics({
    organization,
    liveStream,
    period,
    analytics,
}: Props) {
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
            {
                title: 'Viewer analytics',
                href: viewOrgLiveStreamAnalytics({
                    organization: organization.id,
                    liveStream: liveStream.id,
                }),
            },
        ],
    });

    const changePeriod = (value: string) => {
        if (!periods.some((item) => item.value === value) || value === period) {
            return;
        }

        router.get(
            viewOrgLiveStreamAnalytics({
                organization: organization.id,
                liveStream: liveStream.id,
            }).url,
            { period: value },
            {
                only: ['period', 'analytics'],
                preserveScroll: true,
                preserveState: true,
                replace: true,
            },
        );
    };

    const summaryCards = [
        {
            label: 'Peak concurrent',
            value: analytics.summary.peak_viewers,
            description: 'Highest simultaneous viewer count in this range.',
            icon: Eye,
        },
        {
            label: 'Broadcasts active',
            value: analytics.summary.broadcasts,
            description: 'Stream sessions that overlapped this range.',
            icon: ListVideo,
        },
        {
            label: 'Viewer identities',
            value: analytics.summary.viewer_identity_additions,
            description:
                'New identities within publishing sessions; not unique people.',
            icon: Users,
        },
        {
            label: 'Playback requests',
            value: analytics.summary.playback_requests,
            description: 'Playlist and media requests recorded in this range.',
            icon: MousePointerClick,
        },
    ];

    return (
        <>
            <Head title={`${liveStream.title} viewer analytics`} />

            <div className="flex h-full flex-1 flex-col gap-4 overflow-x-auto rounded-xl p-4">
                <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                    <Heading
                        variant="page"
                        title="Viewer analytics"
                        description={`${liveStream.title} · ${analytics.range_label}`}
                    />
                    <div className="flex flex-wrap items-center gap-2">
                        <Badge variant="outline">
                            {liveStream.status_label}
                        </Badge>
                        <Button asChild variant="outline">
                            <Link
                                href={
                                    showOrgLiveStream({
                                        organization: organization.id,
                                        liveStream: liveStream.id,
                                    }).url
                                }
                            >
                                <ArrowLeft data-icon="inline-start" />
                                Stream details
                            </Link>
                        </Button>
                    </div>
                </div>

                <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                    {summaryCards.map((card) => {
                        const Icon = card.icon;

                        return (
                            <Card key={card.label}>
                                <CardHeader>
                                    <CardTitle className="flex items-center gap-2">
                                        <Icon className="size-3.5 text-muted-foreground" />
                                        {card.label}
                                    </CardTitle>
                                    <CardDescription>
                                        {card.description}
                                    </CardDescription>
                                </CardHeader>
                                <CardContent>
                                    <p className="text-2xl font-semibold tabular-nums">
                                        {card.value.toLocaleString()}
                                    </p>
                                </CardContent>
                            </Card>
                        );
                    })}
                </div>

                <Card>
                    <CardHeader>
                        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                            <div className="flex flex-col gap-1">
                                <CardTitle>Peak concurrent viewers</CardTitle>
                                <CardDescription>
                                    Each point is the highest number of people
                                    watching at the same time within that{' '}
                                    {analytics.granularity}.
                                </CardDescription>
                            </div>
                            <ToggleGroup
                                type="single"
                                value={period}
                                variant="outline"
                                spacing={0}
                                onValueChange={changePeriod}
                                aria-label="Viewer analytics period"
                            >
                                {periods.map((item) => (
                                    <ToggleGroupItem
                                        key={item.value}
                                        value={item.value}
                                        aria-label={`Show ${item.label.toLowerCase()} analytics`}
                                    >
                                        {item.label}
                                    </ToggleGroupItem>
                                ))}
                            </ToggleGroup>
                        </div>
                    </CardHeader>
                    <CardContent className="flex flex-col gap-3">
                        <ViewerLineChart
                            points={analytics.points}
                            period={period}
                        />
                        <p className="text-muted-foreground">
                            {analytics.source_note}
                        </p>
                    </CardContent>
                </Card>
            </div>
        </>
    );
}
