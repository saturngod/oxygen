import { Head, setLayoutProps } from '@inertiajs/react';
import { MoreHorizontal, Plus } from 'lucide-react';
import { useState } from 'react';
import Heading from '@/components/heading';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { ControlFilter } from '@/components/control-filter';
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table';
import { index as indexOrgUsers } from '@/routes/admin/organizations/users';

type User = {
    id: number;
    name: string;
    email: string;
    avatar?: string;
    role: string;
    created_at: string;
};

export default function OrganizationUsers({
    organization,
    users,
}: {
    organization: {
        id: string;
        name: string;
    };
    users: User[];
}) {
    const [search, setSearch] = useState('');

    setLayoutProps({
        breadcrumbs: [
            {
                title: 'Users',
                href: indexOrgUsers({ organization: organization.id }),
            },
        ],
    });

    const filteredUsers = users.filter(
        (user) =>
            user.name.toLowerCase().includes(search.toLowerCase()) ||
            user.email.toLowerCase().includes(search.toLowerCase()),
    );

    return (
        <>
            <Head title="Users" />

            <h1 className="sr-only">Users</h1>

            <div className="flex h-full flex-1 flex-col gap-4 overflow-x-auto rounded-xl p-4">
                <Heading
                    variant="page"
                    title="Users"
                    description={`Manage users in ${organization.name}`}
                />

                <ControlFilter
                    searchValue={search}
                    onSearchChange={setSearch}
                    searchPlaceholder="Search users..."
                    actions={[
                        {
                            label: 'Add User',
                            icon: <Plus className="size-3.5" />,
                        },
                    ]}
                />

                <div className="rounded-lg border">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead>Name</TableHead>
                                <TableHead>Email</TableHead>
                                <TableHead>Role</TableHead>
                                <TableHead className="w-10">
                                    <span className="sr-only">Actions</span>
                                </TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {filteredUsers.map((user) => (
                                <TableRow key={user.id}>
                                    <TableCell className="font-medium">
                                        {user.name}
                                    </TableCell>
                                    <TableCell className="text-muted-foreground">
                                        {user.email}
                                    </TableCell>
                                    <TableCell>
                                        <Badge
                                            variant="secondary"
                                            className="capitalize"
                                        >
                                            {user.role}
                                        </Badge>
                                    </TableCell>
                                    <TableCell>
                                        <DropdownMenu>
                                            <DropdownMenuTrigger asChild>
                                                <Button
                                                    variant="ghost"
                                                    className="size-8 p-0"
                                                >
                                                    <MoreHorizontal className="size-4" />
                                                    <span className="sr-only">
                                                        Open menu
                                                    </span>
                                                </Button>
                                            </DropdownMenuTrigger>
                                            <DropdownMenuContent align="end">
                                                <DropdownMenuItem>
                                                    Edit Profile
                                                </DropdownMenuItem>
                                                <DropdownMenuItem>
                                                    Edit Password
                                                </DropdownMenuItem>
                                                <DropdownMenuSeparator />
                                                <DropdownMenuItem variant="destructive">
                                                    Delete User
                                                </DropdownMenuItem>
                                            </DropdownMenuContent>
                                        </DropdownMenu>
                                    </TableCell>
                                </TableRow>
                            ))}

                            {filteredUsers.length === 0 && (
                                <TableRow>
                                    <TableCell
                                        colSpan={4}
                                        className="h-24 text-center"
                                    >
                                        No users found.
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
