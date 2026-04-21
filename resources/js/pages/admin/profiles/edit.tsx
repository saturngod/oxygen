import { Form, Head, setLayoutProps } from '@inertiajs/react';
import { useMemo, useState } from 'react';
import OrganizationProfilesController from '@/actions/App/Http/Controllers/Admin/OrganizationProfilesController';
import Heading from '@/components/heading';
import InputError from '@/components/input-error';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
    edit as editOrgProfile,
    index as indexOrgProfiles,
} from '@/routes/admin/organizations/profiles';

type Quality = {
    value: string;
    category: string;
    label: string;
    width: number;
    height: number;
    bitrate_kbps: number;
};

const CATEGORY_ORDER = ['SD', 'HD', '4K'] as const;

export default function EditProfile({
    organization,
    profile,
    qualities,
}: {
    organization: {
        id: string;
        name: string;
    };
    profile: {
        id: string;
        name: string;
        qualities: string[];
        is_default: boolean;
    };
    qualities: Quality[];
}) {
    const [selected, setSelected] = useState<string[]>(profile.qualities);

    setLayoutProps({
        breadcrumbs: [
            {
                title: 'Profiles',
                href: indexOrgProfiles({ organization: organization.id }),
            },
            {
                title: profile.name,
                href: editOrgProfile({
                    organization: organization.id,
                    profile: profile.id,
                }),
            },
        ],
    });

    const grouped = useMemo(() => {
        const groups = new Map<string, Quality[]>();

        for (const quality of qualities) {
            const list = groups.get(quality.category) ?? [];
            list.push(quality);
            groups.set(quality.category, list);
        }

        return CATEGORY_ORDER.filter((category) => groups.has(category)).map(
            (category) => ({
                category,
                items: groups.get(category)!,
            }),
        );
    }, [qualities]);

    const toggle = (value: string) => {
        setSelected((current) =>
            current.includes(value)
                ? current.filter((item) => item !== value)
                : [...current, value],
        );
    };

    const formatBitrate = (kbps: number) =>
        kbps >= 1_000
            ? `${(kbps / 1_000).toLocaleString(undefined, { maximumFractionDigits: 1 })} Mbps`
            : `${kbps.toLocaleString()} kbps`;

    return (
        <>
            <Head title={`Edit ${profile.name}`} />

            <h1 className="sr-only">Edit {profile.name}</h1>

            <div className="flex h-full flex-1 flex-col gap-4 overflow-x-auto rounded-xl p-4">
                <Heading
                    variant="page"
                    title="Edit Coding Profile"
                    description={`Update encoding profile for ${organization.name}`}
                />

                <Form
                    {...OrganizationProfilesController.update.form({
                        organization: organization.id,
                        profile: profile.id,
                    })}
                    options={{ preserveScroll: true }}
                    className="space-y-6"
                >
                    {({ processing, errors }) => (
                        <>
                            <div className="grid gap-2">
                                <Label htmlFor="name">Name</Label>
                                <Input
                                    id="name"
                                    name="name"
                                    required
                                    defaultValue={profile.name}
                                    placeholder="e.g. Standard Web Delivery"
                                    className="mt-1 block w-full max-w-md"
                                />
                                <InputError
                                    className="mt-2"
                                    message={errors.name}
                                />
                            </div>

                            <div className="grid gap-3">
                                <Label>Video qualities</Label>
                                <p className="text-xs text-muted-foreground">
                                    Select one or more output renditions. Each
                                    selected rendition will be generated when a
                                    video is encoded with this profile.
                                </p>

                                {selected.map((value) => (
                                    <input
                                        key={value}
                                        type="hidden"
                                        name="qualities[]"
                                        value={value}
                                    />
                                ))}

                                <div className="grid gap-4 lg:grid-cols-3">
                                    {grouped.map(({ category, items }) => (
                                        <Card key={category} size="sm">
                                            <CardHeader>
                                                <CardTitle>
                                                    {category}
                                                </CardTitle>
                                            </CardHeader>
                                            <CardContent className="flex flex-col gap-2">
                                                {items.map((quality) => {
                                                    const isChecked =
                                                        selected.includes(
                                                            quality.value,
                                                        );

                                                    return (
                                                        <label
                                                            key={quality.value}
                                                            data-checked={
                                                                isChecked
                                                            }
                                                            className="flex cursor-pointer items-center justify-between gap-3 rounded-md border border-border bg-background px-3 py-2 transition-colors hover:bg-accent/40 data-[checked=true]:border-primary/60 data-[checked=true]:bg-primary/5"
                                                        >
                                                            <div className="flex items-center gap-3">
                                                                <Checkbox
                                                                    checked={
                                                                        isChecked
                                                                    }
                                                                    onCheckedChange={() =>
                                                                        toggle(
                                                                            quality.value,
                                                                        )
                                                                    }
                                                                />
                                                                <span className="text-xs font-medium text-foreground">
                                                                    {
                                                                        quality.label
                                                                    }
                                                                </span>
                                                            </div>
                                                            <span className="text-xs text-muted-foreground tabular-nums">
                                                                {formatBitrate(
                                                                    quality.bitrate_kbps,
                                                                )}
                                                            </span>
                                                        </label>
                                                    );
                                                })}
                                            </CardContent>
                                        </Card>
                                    ))}
                                </div>

                                <InputError
                                    className="mt-2"
                                    message={
                                        errors.qualities ??
                                        (errors as Record<string, string>)[
                                            'qualities.0'
                                        ]
                                    }
                                />
                            </div>

                            <div className="flex items-center gap-4">
                                <Button
                                    disabled={
                                        processing || selected.length === 0
                                    }
                                    data-test="update-profile-button"
                                >
                                    Save changes
                                </Button>
                            </div>
                        </>
                    )}
                </Form>
            </div>
        </>
    );
}
