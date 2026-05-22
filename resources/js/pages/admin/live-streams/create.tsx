import { Form, Head, setLayoutProps } from '@inertiajs/react';
import { Radio } from 'lucide-react';
import { useState } from 'react';
import OrganizationLiveStreamsController from '@/actions/App/Http/Controllers/Admin/OrganizationLiveStreamsController';
import Heading from '@/components/heading';
import InputError from '@/components/input-error';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
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
    const [recordingEnabled, setRecordingEnabled] = useState(false);

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

                            <input
                                type="hidden"
                                name="recording_enabled"
                                value={recordingEnabled ? '1' : '0'}
                            />

                            <div className="flex items-start gap-3 rounded-lg border p-3">
                                <Checkbox
                                    id="recording_enabled"
                                    checked={recordingEnabled}
                                    onCheckedChange={(checked) =>
                                        setRecordingEnabled(checked === true)
                                    }
                                />
                                <div className="grid gap-1">
                                    <Label htmlFor="recording_enabled">
                                        Record this stream
                                    </Label>
                                    <p className="text-xs text-muted-foreground">
                                        The Go live service will keep a full
                                        session recording when this is enabled.
                                    </p>
                                </div>
                            </div>

                            <Button disabled={processing}>Create Stream</Button>
                        </>
                    )}
                </Form>
            </div>
        </>
    );
}
