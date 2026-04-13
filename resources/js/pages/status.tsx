import { Head } from '@inertiajs/react';
import Heading from '@/components/heading';
import { PlaceholderPattern } from '@/components/ui/placeholder-pattern';

export default function Status() {
    return (
        <>
            <Head title="Status" />
            <div className="flex h-full flex-1 flex-col gap-4 p-4">
                <Heading
                    variant="page"
                    title="Status"
                    description="Transcode status and job progress."
                />
                <div className="relative min-h-[60vh] flex-1 overflow-hidden rounded-xl border border-sidebar-border/70 dark:border-sidebar-border">
                    <PlaceholderPattern className="absolute inset-0 size-full stroke-neutral-900/20 dark:stroke-neutral-100/20" />
                </div>
            </div>
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
