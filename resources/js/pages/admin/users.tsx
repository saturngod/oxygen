import { Head, setLayoutProps } from '@inertiajs/react';
import Heading from '@/components/heading';
import { PlaceholderPattern } from '@/components/ui/placeholder-pattern';
import { index as indexOrgUsers } from '@/routes/admin/organizations/users';

export default function OrganizationUsers({
    organization,
}: {
    organization: {
        id: number;
        name: string;
    };
}) {
    setLayoutProps({
        breadcrumbs: [
            {
                title: 'Users',
                href: indexOrgUsers({ organization: organization.id }),
            },
        ],
    });

    return (
        <>
            <Head title="Users" />

            <h1 className="sr-only">Users</h1>

            <div className="flex h-full flex-1 flex-col gap-4 overflow-x-auto rounded-xl p-4">
                <Heading
                    variant="small"
                    title="Users"
                    description={`Manage users in ${organization.name}`}
                />

                <div className="relative min-h-[40vh] flex-1 overflow-hidden rounded-xl border border-sidebar-border/70 dark:border-sidebar-border">
                    <PlaceholderPattern className="absolute inset-0 size-full stroke-neutral-900/20 dark:stroke-neutral-100/20" />
                </div>
            </div>
        </>
    );
}
