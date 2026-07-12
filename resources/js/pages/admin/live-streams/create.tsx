import { Form, Head, setLayoutProps } from '@inertiajs/react';
import { Radio } from 'lucide-react';
import OrganizationLiveStreamsController from '@/actions/App/Http/Controllers/Admin/OrganizationLiveStreamsController';
import Heading from '@/components/heading';
import InputError from '@/components/input-error';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
    create as createOrgLiveStream,
    index as indexOrgLiveStreams,
} from '@/routes/admin/organizations/live-streams';

type Props = {
    organization: {
        id: string;
        name: string;
    };
};

export default function CreateLiveStream({ organization }: Props) {
    setLayoutProps({
        breadcrumbs: [
            {
                title: 'Live Streams',
                href: indexOrgLiveStreams({ organization: organization.id }),
            },
            {
                title: 'Create',
                href: createOrgLiveStream({ organization: organization.id }),
            },
        ],
    });

    return (
        <>
            <Head title="New Live Stream" />

            <h1 className="sr-only">New Live Stream</h1>

            <div className="flex h-full flex-1 flex-col gap-4 overflow-x-auto rounded-xl p-4">
                <Heading
                    variant="page"
                    title="New Live Stream"
                    description={`Create RTMP credentials for ${organization.name}`}
                />

                <Form
                    {...OrganizationLiveStreamsController.store.form({
                        organization: organization.id,
                    })}
                    options={{ preserveScroll: true }}
                    className="max-w-2xl space-y-6"
                >
                    {({ processing, errors }) => (
                        <>
                            <div className="grid gap-2">
                                <Label htmlFor="title">
                                    <Radio className="mr-1 inline size-3.5" />
                                    Title
                                </Label>
                                <Input
                                    id="title"
                                    name="title"
                                    required
                                    placeholder="Friday broadcast"
                                />
                                <InputError message={errors.title} />
                            </div>

                            <Button disabled={processing}>Create Stream</Button>
                        </>
                    )}
                </Form>
            </div>
        </>
    );
}
