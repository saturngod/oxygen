import { Head } from '@inertiajs/react';
import Heading from '@/components/heading';
import { PlaceholderPattern } from '@/components/ui/placeholder-pattern';

export default function Manage() {
    return (
        <>
            <Head title="Manage" />
            <div className="flex h-full flex-1 flex-col gap-4 p-4">
                <Heading
                    variant="page"
                    title="Manage"
                    description="Create folders and upload videos (mp4, mov)."
                />
                <div className="relative min-h-[60vh] flex-1 overflow-hidden rounded-xl border border-sidebar-border/70 dark:border-sidebar-border">
                    <PlaceholderPattern className="absolute inset-0 size-full stroke-neutral-900/20 dark:stroke-neutral-100/20" />
                </div>
            </div>
        </>
    );
}

Manage.layout = {
    breadcrumbs: [
        {
            title: 'Manage',
            href: '/manage',
        },
    ],
};
