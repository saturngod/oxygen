import { Link, usePage } from '@inertiajs/react';
import { Check, ChevronsUpDown } from 'lucide-react';
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar';
import { Button } from '@/components/ui/button';
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuLabel,
    DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { useInitials } from '@/hooks/use-initials';
import { cn } from '@/lib/utils';
import organizations from '@/routes/organizations';
import type { SharedData } from '@/types';

export function OrganizationSwitcher({ className }: { className?: string }) {
    const page = usePage<SharedData>();
    const getInitials = useInitials();
    const current = page.props.auth.user?.current_organization ?? null;
    const memberships = page.props.auth.organizations ?? [];

    if (memberships.length === 0) {
        return null;
    }

    const displayName = current?.name ?? memberships[0].name;

    return (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <Button
                    variant="ghost"
                    className={cn(
                        'flex h-10 items-center gap-2 px-2 text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground',
                        className,
                    )}
                >
                    <Avatar className="h-7 w-7 !rounded-md after:!rounded-md">
                        {current?.image_url && (
                            <AvatarImage
                                src={current.image_url}
                                alt={displayName}
                                className="!rounded-md"
                            />
                        )}
                        <AvatarFallback className="!rounded-md bg-neutral-200 text-xs font-semibold text-black dark:bg-neutral-700 dark:text-white">
                            {getInitials(displayName)}
                        </AvatarFallback>
                    </Avatar>
                    <span className="flex-1 truncate text-left text-sm font-medium">
                        {displayName}
                    </span>
                    <ChevronsUpDown className="ml-auto h-4 w-4 shrink-0 opacity-70" />
                </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent
                align="start"
                sideOffset={4}
                className="w-(--radix-dropdown-menu-trigger-width) p-0"
            >
                <DropdownMenuLabel className="px-2 pt-2 pb-1 text-xs text-muted-foreground">
                    Organizations
                </DropdownMenuLabel>
                {memberships.map((membership) => {
                    const isActive = membership.id === current?.id;

                    return (
                        <DropdownMenuItem
                            key={membership.id}
                            asChild
                            className="rounded-none px-2 py-2"
                        >
                            <Link
                                href={
                                    organizations.switch({
                                        organization: membership.id,
                                    }).url
                                }
                                method="put"
                                as="button"
                                preserveScroll
                                className="flex w-full items-center gap-2"
                            >
                                <Avatar className="h-7 w-7 !rounded-md after:!rounded-md">
                                    {membership.image_url && (
                                        <AvatarImage
                                            src={membership.image_url}
                                            alt={membership.name}
                                            className="!rounded-md"
                                        />
                                    )}
                                    <AvatarFallback className="!rounded-md bg-neutral-200 text-xs font-semibold text-black dark:bg-neutral-700 dark:text-white">
                                        {getInitials(membership.name)}
                                    </AvatarFallback>
                                </Avatar>
                                <div className="flex flex-1 flex-col text-left">
                                    <span className="truncate text-sm font-medium">
                                        {membership.name}
                                    </span>
                                    <span className="truncate text-xs text-muted-foreground capitalize">
                                        {membership.role}
                                    </span>
                                </div>
                                {isActive && (
                                    <Check className="h-4 w-4 text-muted-foreground" />
                                )}
                            </Link>
                        </DropdownMenuItem>
                    );
                })}
            </DropdownMenuContent>
        </DropdownMenu>
    );
}
