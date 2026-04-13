import { Form, Head, setLayoutProps, usePage } from '@inertiajs/react';
import OrganizationSettingsController from '@/actions/App/Http/Controllers/Admin/OrganizationSettingsController';
import Heading from '@/components/heading';
import InputError from '@/components/input-error';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { edit as editOrgSettings } from '@/routes/admin/organizations/settings';
import type { SharedData } from '@/types';

export default function OrganizationSettings({
    organization,
}: {
    organization: {
        id: number;
        name: string;
        image_url: string;
        contact_email: string | null;
        phone: string | null;
        address: string | null;
    };
}) {
    const { auth } = usePage<SharedData>().props;

    setLayoutProps({
        breadcrumbs: [
            {
                title: 'Organization settings',
                href: editOrgSettings({ organization: organization.id }),
            },
        ],
    });

    return (
        <>
            <Head title="Organization settings" />

            <h1 className="sr-only">Organization settings</h1>

            <div className="flex h-full flex-1 flex-col gap-4 overflow-x-auto rounded-xl p-4">
                <Heading
                    variant="small"
                    title="Organization information"
                    description="Update your organization's name, logo, and contact details"
                />

                <Form
                    {...OrganizationSettingsController.update.form({
                        organization: organization.id,
                    })}
                    options={{ preserveScroll: true }}
                    className="space-y-6"
                >
                    {({ processing, errors }) => (
                        <>
                            <div className="grid gap-2">
                                <Label htmlFor="image">Logo</Label>

                                {organization.image_url && (
                                    <img
                                        src={organization.image_url}
                                        alt={organization.name}
                                        className="h-16 w-16 rounded-md object-cover"
                                    />
                                )}

                                <Input
                                    id="image"
                                    type="file"
                                    className="mt-1 block w-full"
                                    name="image"
                                />

                                <InputError
                                    className="mt-2"
                                    message={errors.image}
                                />
                            </div>

                            <div className="grid gap-2">
                                <Label htmlFor="name">Name</Label>

                                <Input
                                    id="name"
                                    className="mt-1 block w-full"
                                    defaultValue={organization.name}
                                    name="name"
                                    required
                                    placeholder="Organization name"
                                />

                                <InputError
                                    className="mt-2"
                                    message={errors.name}
                                />
                            </div>

                            <div className="grid gap-2">
                                <Label htmlFor="contact_email">
                                    Contact email
                                </Label>

                                <Input
                                    id="contact_email"
                                    type="email"
                                    className="mt-1 block w-full"
                                    defaultValue={
                                        organization.contact_email ?? ''
                                    }
                                    name="contact_email"
                                    placeholder="contact@example.com"
                                />

                                <InputError
                                    className="mt-2"
                                    message={errors.contact_email}
                                />
                            </div>

                            <div className="grid gap-2">
                                <Label htmlFor="phone">Phone</Label>

                                <Input
                                    id="phone"
                                    className="mt-1 block w-full"
                                    defaultValue={organization.phone ?? ''}
                                    name="phone"
                                    placeholder="+1 (555) 000-0000"
                                />

                                <InputError
                                    className="mt-2"
                                    message={errors.phone}
                                />
                            </div>

                            <div className="grid gap-2">
                                <Label htmlFor="address">Address</Label>

                                <Input
                                    id="address"
                                    className="mt-1 block w-full"
                                    defaultValue={organization.address ?? ''}
                                    name="address"
                                    placeholder="123 Main St, City, State"
                                />

                                <InputError
                                    className="mt-2"
                                    message={errors.address}
                                />
                            </div>

                            <div className="flex items-center gap-4">
                                <Button
                                    disabled={processing}
                                    data-test="update-organization-settings-button"
                                >
                                    Save
                                </Button>
                            </div>
                        </>
                    )}
                </Form>
            </div>
        </>
    );
}
