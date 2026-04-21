import { Head, router, setLayoutProps } from '@inertiajs/react';
import { CheckCircle2, Pencil, Plus, Star } from 'lucide-react';
import { useState } from 'react';
import OrganizationProfilesController from '@/actions/App/Http/Controllers/Admin/OrganizationProfilesController';
import { ControlFilter } from '@/components/control-filter';
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
import {
    create as createOrgProfile,
    edit as editOrgProfile,
    index as indexOrgProfiles,
} from '@/routes/admin/organizations/profiles';

type Profile = {
    id: string;
    name: string;
    qualities: string[];
    is_default: boolean;
    created_at: string;
};

export default function OrganizationProfiles({
    organization,
    profiles,
}: {
    organization: {
        id: string;
        name: string;
    };
    profiles: Profile[];
}) {
    const [search, setSearch] = useState('');
    const [pendingDefault, setPendingDefault] = useState<Profile | null>(null);
    const [processing, setProcessing] = useState(false);

    setLayoutProps({
        breadcrumbs: [
            {
                title: 'Profiles',
                href: indexOrgProfiles({ organization: organization.id }),
            },
        ],
    });

    const filteredProfiles = profiles.filter((profile) =>
        profile.name.toLowerCase().includes(search.toLowerCase()),
    );

    const confirmMakeDefault = () => {
        if (!pendingDefault) {
            return;
        }

        router.put(
            OrganizationProfilesController.makeDefault.url({
                organization: organization.id,
                profile: pendingDefault.id,
            }),
            {},
            {
                preserveScroll: true,
                onStart: () => setProcessing(true),
                onFinish: () => {
                    setProcessing(false);
                    setPendingDefault(null);
                },
            },
        );
    };

    return (
        <>
            <Head title="Coding Profiles" />

            <h1 className="sr-only">Coding Profiles</h1>

            <div className="flex h-full flex-1 flex-col gap-4 overflow-x-auto rounded-xl p-4">
                <Heading
                    variant="page"
                    title="Coding Profiles"
                    description={`Manage encoding profiles in ${organization.name}`}
                />

                <ControlFilter
                    searchValue={search}
                    onSearchChange={setSearch}
                    searchPlaceholder="Search profiles..."
                    actions={[
                        {
                            label: 'Add Profile',
                            icon: <Plus className="size-3.5" />,
                            onClick: () =>
                                router.visit(
                                    createOrgProfile({
                                        organization: organization.id,
                                    }),
                                ),
                        },
                    ]}
                />

                <div className="rounded-lg border">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead className="w-32">Default</TableHead>
                                <TableHead>Name</TableHead>
                                <TableHead>Qualities</TableHead>
                                <TableHead className="w-40">Created</TableHead>
                                <TableHead className="w-40 text-right">
                                    <span className="sr-only">Actions</span>
                                </TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {filteredProfiles.map((profile) => (
                                <TableRow key={profile.id}>
                                    <TableCell>
                                        {profile.is_default ? (
                                            <Badge className="gap-1">
                                                <CheckCircle2 className="size-3.5" />
                                                Default
                                            </Badge>
                                        ) : (
                                            <span className="text-muted-foreground">
                                                —
                                            </span>
                                        )}
                                    </TableCell>
                                    <TableCell className="font-medium">
                                        {profile.name}
                                    </TableCell>
                                    <TableCell>
                                        <div className="flex flex-wrap gap-1">
                                            {profile.qualities.map(
                                                (quality) => (
                                                    <Badge
                                                        key={quality}
                                                        variant="secondary"
                                                    >
                                                        {quality}
                                                    </Badge>
                                                ),
                                            )}
                                        </div>
                                    </TableCell>
                                    <TableCell className="text-muted-foreground">
                                        {new Date(
                                            profile.created_at,
                                        ).toLocaleDateString()}
                                    </TableCell>
                                    <TableCell className="text-right">
                                        <div className="flex items-center justify-end gap-2">
                                            <Button
                                                variant="ghost"
                                                size="sm"
                                                onClick={() =>
                                                    router.visit(
                                                        editOrgProfile({
                                                            organization:
                                                                organization.id,
                                                            profile: profile.id,
                                                        }),
                                                    )
                                                }
                                            >
                                                <Pencil className="size-3.5" />
                                            </Button>
                                            {!profile.is_default && (
                                                <Button
                                                    variant="outline"
                                                    size="sm"
                                                    onClick={() =>
                                                        setPendingDefault(
                                                            profile,
                                                        )
                                                    }
                                                >
                                                    <Star className="size-3.5" />
                                                    Make default
                                                </Button>
                                            )}
                                        </div>
                                    </TableCell>
                                </TableRow>
                            ))}

                            {filteredProfiles.length === 0 && (
                                <TableRow>
                                    <TableCell
                                        colSpan={5}
                                        className="h-24 text-center"
                                    >
                                        No profiles found.
                                    </TableCell>
                                </TableRow>
                            )}
                        </TableBody>
                    </Table>
                </div>
            </div>

            <Dialog
                open={pendingDefault !== null}
                onOpenChange={(open) => {
                    if (!open) {
                        setPendingDefault(null);
                    }
                }}
            >
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Make default profile?</DialogTitle>
                        <DialogDescription>
                            {pendingDefault ? (
                                <>
                                    Set{' '}
                                    <span className="font-medium text-foreground">
                                        {pendingDefault.name}
                                    </span>{' '}
                                    as the default encoding profile for this
                                    organization. New uploads will use this
                                    profile unless another is chosen.
                                </>
                            ) : null}
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <DialogClose asChild>
                            <Button variant="outline" disabled={processing}>
                                Cancel
                            </Button>
                        </DialogClose>
                        <Button
                            onClick={confirmMakeDefault}
                            disabled={processing}
                            data-test="confirm-make-default-button"
                        >
                            Confirm
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </>
    );
}
