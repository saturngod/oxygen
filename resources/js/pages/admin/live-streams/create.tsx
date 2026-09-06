import { Form, Head, Link, setLayoutProps } from '@inertiajs/react';
import { Radio } from 'lucide-react';
import OrganizationLiveStreamsController from '@/actions/App/Http/Controllers/Admin/OrganizationLiveStreamsController';
import Heading from '@/components/heading';
import InputError from '@/components/input-error';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import {
    create as createOrgLiveStream,
    index as indexOrgLiveStreams,
} from '@/routes/admin/organizations/live-streams';
import { create as createOrgProfile } from '@/routes/admin/organizations/profiles';

type Props = {
    organization: {
        id: string;
        name: string;
    };
    profiles: {
        id: string;
        name: string;
        qualities: string[];
        is_default: boolean;
    }[];
};

export default function CreateLiveStream({ organization, profiles }: Props) {
    const defaultProfile =
        profiles.find((profile) => profile.is_default) ?? profiles[0];
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

                            <div className="grid gap-2">
                                <Label htmlFor="profile_id">
                                    Encoding profile
                                </Label>
                                {defaultProfile ? (
                                    <Select
                                        name="profile_id"
                                        defaultValue={defaultProfile.id}
                                        required
                                    >
                                        <SelectTrigger
                                            id="profile_id"
                                            className="w-full"
                                        >
                                            <SelectValue placeholder="Select a profile" />
                                        </SelectTrigger>
                                        <SelectContent>
                                            {profiles.map((profile) => (
                                                <SelectItem
                                                    key={profile.id}
                                                    value={profile.id}
                                                >
                                                    <span>{profile.name}</span>
                                                    <span className="ml-2 text-xs text-muted-foreground">
                                                        {profile.qualities
                                                            .length === 0
                                                            ? 'Passthrough · live direct'
                                                            : profile.qualities.join(
                                                                  ' · ',
                                                              )}
                                                    </span>
                                                </SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                ) : (
                                    <p className="rounded-md border border-dashed p-3 text-sm text-muted-foreground">
                                        Create an encoding profile before
                                        creating a live stream.{' '}
                                        <Link
                                            href={
                                                createOrgProfile({
                                                    organization:
                                                        organization.id,
                                                }).url
                                            }
                                            className="font-medium text-foreground underline underline-offset-4"
                                        >
                                            Create profile
                                        </Link>
                                    </p>
                                )}
                                <p className="text-xs text-muted-foreground">
                                    The live service generates one HLS rendition
                                    for each quality in this profile and
                                    publishes them through the master playlist.
                                    A passthrough profile forwards the source
                                    stream without transcoding.
                                </p>
                                <InputError message={errors.profile_id} />
                            </div>

                            <Button disabled={processing || !defaultProfile}>
                                Create Stream
                            </Button>
                        </>
                    )}
                </Form>
            </div>
        </>
    );
}
