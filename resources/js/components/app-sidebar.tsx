import {
    Activity,
    BookOpen,
    FolderGit2,
    FolderKanban,
    LayoutGrid,
    Settings,
    SlidersHorizontal,
    Users,
} from 'lucide-react';
import { usePage } from '@inertiajs/react';
import { NavFooter } from '@/components/nav-footer';
import { NavMain } from '@/components/nav-main';
import { NavUser } from '@/components/nav-user';
import { OrganizationSwitcher } from '@/components/organization-switcher';
import {
    Sidebar,
    SidebarContent,
    SidebarFooter,
    SidebarHeader,
    SidebarMenu,
    SidebarMenuItem,
} from '@/components/ui/sidebar';
import { dashboard, manage, status } from '@/routes';
import { index as indexOrgProfiles } from '@/routes/admin/organizations/profiles';
import { edit as editOrgSettings } from '@/routes/admin/organizations/settings';
import { index as indexOrgUsers } from '@/routes/admin/organizations/users';
import type { NavItem, SharedData } from '@/types';

const mainNavItems: NavItem[] = [
    {
        title: 'Dashboard',
        href: dashboard(),
        icon: LayoutGrid,
    },
    {
        title: 'Manage',
        href: manage(),
        icon: FolderKanban,
    },
    {
        title: 'Status',
        href: status(),
        icon: Activity,
    },
];

const footerNavItems: NavItem[] = [
    {
        title: 'Repository',
        href: 'https://github.com/laravel/react-starter-kit',
        icon: FolderGit2,
    },
    {
        title: 'Documentation',
        href: 'https://laravel.com/docs/starter-kits#react',
        icon: BookOpen,
    },
];

export function AppSidebar() {
    const { auth } = usePage<SharedData>().props;
    const isAdmin = auth.user.current_role === 'admin';
    const orgId = auth.user.current_organization?.id;

    const adminNavItems: NavItem[] =
        isAdmin && orgId
            ? [
                  {
                      title: 'Users',
                      href: indexOrgUsers({ organization: orgId }),
                      icon: Users,
                  },
                  {
                      title: 'Profiles',
                      href: indexOrgProfiles({ organization: orgId }),
                      icon: SlidersHorizontal,
                  },
                  {
                      title: 'Settings',
                      href: editOrgSettings({ organization: orgId }),
                      icon: Settings,
                  },
              ]
            : [];

    return (
        <Sidebar collapsible="icon" variant="inset">
            <SidebarHeader>
                <SidebarMenu>
                    <SidebarMenuItem>
                        <OrganizationSwitcher className="w-full justify-start" />
                    </SidebarMenuItem>
                </SidebarMenu>
            </SidebarHeader>

            <SidebarContent>
                <NavMain title="Platform" items={mainNavItems} />
                {isAdmin && adminNavItems.length > 0 && (
                    <NavMain title="Admin" items={adminNavItems} />
                )}
            </SidebarContent>

            <SidebarFooter>
                <NavFooter items={footerNavItems} className="mt-auto" />
                <NavUser />
            </SidebarFooter>
        </Sidebar>
    );
}
