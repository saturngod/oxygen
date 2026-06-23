import { Form, Head, setLayoutProps } from '@inertiajs/react';
import { Check, Link, Plus, Trash2 } from 'lucide-react';
import { useState } from 'react';
import OrganizationWebhooksController from '@/actions/App/Http/Controllers/Admin/OrganizationWebhooksController';
import { ControlFilter } from '@/components/control-filter';
import Heading from '@/components/heading';
import InputError from '@/components/input-error';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import {
    Dialog,
    DialogClose,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table';
import { index as indexOrgWebhooks } from '@/routes/admin/organizations/webhooks';

type Webhook = {
    id: string;
    url: string;
    events: string[];
    is_active: boolean;
    created_at: string;
};

type AvailableEvent = {
    value: string;
    label: string;
};

export default function OrganizationWebhooks({
    organization,
    webhooks,
    availableEvents,
}: {
    organization: {
        id: string;
        name: string;
    };
    webhooks: Webhook[];
    availableEvents: AvailableEvent[];
}) {
    const [search, setSearch] = useState('');
    const [showCreate, setShowCreate] = useState(false);
    const [selectedEvents, setSelectedEvents] = useState<string[]>([]);
    const [pendingDelete, setPendingDelete] = useState<Webhook | null>(null);
    const [processing, setProcessing] = useState(false);

    setLayoutProps({
        breadcrumbs: [
            {
                title: 'Webhooks',
                href: indexOrgWebhooks({ organization: organization.id }),
            },
        ],
    });

    const filteredWebhooks = webhooks.filter((webhook) =>
        webhook.url.toLowerCase().includes(search.toLowerCase()),
    );

    const toggleEvent = (value: string) => {
        setSelectedEvents((current) =>
            current.includes(value)
                ? current.filter((e) => e !== value)
                : [...current, value],
        );
    };

    const eventLabel = (value: string) =>
        availableEvents.find((e) => e.value === value)?.label ?? value;

    return (
        <>
            <Head title="Webhooks" />

            <h1 className="sr-only">Webhooks</h1>

            <div className="flex h-full flex-1 flex-col gap-4 overflow-x-auto rounded-xl p-4">
                <Heading
                    variant="page"
                    title="Webhooks"
                    description={`Manage webhooks for ${organization.name}`}
                />

                <ControlFilter
                    searchValue={search}
                    onSearchChange={setSearch}
                    searchPlaceholder="Search webhooks..."
                    actions={[
                        {
                            label: 'Add Webhook',
                            icon: <Plus className="size-3.5" />,
                            onClick: () => {
                                setSelectedEvents([]);
                                setShowCreate(true);
                            },
                        },
                    ]}
                />

                <div className="rounded-lg border">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead>URL</TableHead>
                                <TableHead>Events</TableHead>
                                <TableHead>Status</TableHead>
                                <TableHead className="w-40">Created</TableHead>
                                <TableHead className="w-24 text-right">
                                    <span className="sr-only">Actions</span>
                                </TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {filteredWebhooks.map((webhook) => (
                                <TableRow key={webhook.id}>
                                    <TableCell className="max-w-md truncate font-mono text-sm">
                                        {webhook.url}
                                    </TableCell>
                                    <TableCell>
                                        <div className="flex flex-wrap gap-1">
                                            {webhook.events.map((event) => (
                                                <Badge
                                                    key={event}
                                                    variant="secondary"
                                                >
                                                    {eventLabel(event)}
                                                </Badge>
                                            ))}
                                        </div>
                                    </TableCell>
                                    <TableCell>
                                        {webhook.is_active ? (
                                            <Badge className="gap-1">
                                                <Check className="size-3.5" />
                                                Active
                                            </Badge>
                                        ) : (
                                            <Badge variant="outline">
                                                Inactive
                                            </Badge>
                                        )}
                                    </TableCell>
                                    <TableCell className="text-muted-foreground">
                                        {new Date(
                                            webhook.created_at,
                                        ).toLocaleDateString()}
                                    </TableCell>
                                    <TableCell className="text-right">
                                        <Button
                                            variant="ghost"
                                            size="sm"
                                            onClick={() =>
                                                setPendingDelete(webhook)
                                            }
                                        >
                                            <Trash2 className="size-3.5 text-destructive" />
                                        </Button>
                                    </TableCell>
                                </TableRow>
                            ))}

                            {filteredWebhooks.length === 0 && (
                                <TableRow>
                                    <TableCell
                                        colSpan={5}
                                        className="h-24 text-center"
                                    >
                                        No webhooks found.
                                    </TableCell>
                                </TableRow>
                            )}
                        </TableBody>
                    </Table>
                </div>
            </div>

            <Dialog
                open={showCreate}
                onOpenChange={(open) => {
                    if (!open) {
                        setShowCreate(false);
                    }
                }}
            >
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Add Webhook</DialogTitle>
                        <DialogDescription>
                            Configure a webhook endpoint to receive event
                            notifications.
                        </DialogDescription>
                    </DialogHeader>

                    <Form
                        {...OrganizationWebhooksController.store.form({
                            organization: organization.id,
                        })}
                        options={{ preserveScroll: true }}
                        className="space-y-4"
                        onSuccess={() => setShowCreate(false)}
                    >
                        {({ processing: submitting, errors }) => (
                            <>
                                <div className="grid gap-2">
                                    <Label htmlFor="url">
                                        <Link className="mr-1 inline size-3.5" />
                                        Webhook URL
                                    </Label>
                                    <Input
                                        id="url"
                                        name="url"
                                        type="url"
                                        required
                                        placeholder="https://example.com/webhook"
                                        className="mt-1 block w-full"
                                    />
                                    <InputError
                                        className="mt-2"
                                        message={errors.url}
                                    />
                                </div>

                                <div className="grid gap-2">
                                    <Label>Events</Label>
                                    <p className="text-xs text-muted-foreground">
                                        Select which events should trigger this
                                        webhook.
                                    </p>

                                    {selectedEvents.map((value) => (
                                        <input
                                            key={value}
                                            type="hidden"
                                            name="events[]"
                                            value={value}
                                        />
                                    ))}

                                    <div className="flex flex-col gap-2">
                                        {availableEvents.map((event) => {
                                            const isChecked =
                                                selectedEvents.includes(
                                                    event.value,
                                                );

                                            return (
                                                <label
                                                    key={event.value}
                                                    data-checked={isChecked}
                                                    className="flex cursor-pointer items-center gap-3 rounded-md border border-border bg-background px-3 py-2 transition-colors hover:bg-accent/40 data-[checked=true]:border-primary/60 data-[checked=true]:bg-primary/5"
                                                >
                                                    <Checkbox
                                                        checked={isChecked}
                                                        onCheckedChange={() =>
                                                            toggleEvent(
                                                                event.value,
                                                            )
                                                        }
                                                    />
                                                    <span className="text-sm font-medium text-foreground">
                                                        {event.label}
                                                    </span>
                                                </label>
                                            );
                                        })}
                                    </div>

                                    <InputError
                                        className="mt-2"
                                        message={
                                            errors.events ??
                                            (errors as Record<string, string>)[
                                                'events.0'
                                            ]
                                        }
                                    />
                                </div>

                                <DialogFooter>
                                    <DialogClose asChild>
                                        <Button
                                            variant="outline"
                                            disabled={submitting}
                                        >
                                            Cancel
                                        </Button>
                                    </DialogClose>
                                    <Button
                                        disabled={
                                            submitting ||
                                            selectedEvents.length === 0
                                        }
                                    >
                                        Create webhook
                                    </Button>
                                </DialogFooter>
                            </>
                        )}
                    </Form>
                </DialogContent>
            </Dialog>

            <Dialog
                open={pendingDelete !== null}
                onOpenChange={(open) => {
                    if (!open) {
                        setPendingDelete(null);
                        // Reset here too: onFinish only fires when the in-flight
                        // request completes, so dismissing mid-request would
                        // otherwise leave processing=true and disable the next
                        // webhook's delete dialog.
                        setProcessing(false);
                    }
                }}
            >
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Delete webhook?</DialogTitle>
                        <DialogDescription>
                            {pendingDelete ? (
                                <>
                                    Remove the webhook at{' '}
                                    <span className="font-mono text-foreground">
                                        {pendingDelete.url}
                                    </span>
                                    . This action cannot be undone.
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
                            variant="destructive"
                            disabled={processing}
                            onClick={() => {
                                if (!pendingDelete) {
                                    return;
                                }

                                import('@inertiajs/react').then(
                                    ({ router }) => {
                                        router.delete(
                                            OrganizationWebhooksController.destroy.url(
                                                {
                                                    organization:
                                                        organization.id,
                                                    webhook: pendingDelete.id,
                                                },
                                            ),
                                            {
                                                preserveScroll: true,
                                                onStart: () =>
                                                    setProcessing(true),
                                                onFinish: () => {
                                                    setProcessing(false);
                                                    setPendingDelete(null);
                                                },
                                            },
                                        );
                                    },
                                );
                            }}
                        >
                            Delete
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </>
    );
}
