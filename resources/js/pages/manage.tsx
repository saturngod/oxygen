import { Form, Head, Link, router, setLayoutProps } from '@inertiajs/react';
import {
    ChevronRight,
    Folder as FolderIcon,
    Link as LinkIcon,
    MoreHorizontal,
    Plus,
    Upload,
    UploadCloud,
    X,
} from 'lucide-react';
import {
    useRef,
    useState,
    type ChangeEvent,
    type DragEvent,
    type FormEvent,
    type KeyboardEvent,
} from 'react';
import ManageController from '@/actions/App/Http/Controllers/ManageController';
import Heading from '@/components/heading';
import InputError from '@/components/input-error';
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
    DialogTrigger,
} from '@/components/ui/dialog';
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
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
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table';
import { manage } from '@/routes';

type FolderItem = {
    id: string;
    name: string;
};

type FileStatus = 'uploaded' | 'progress' | 'success' | 'failed';

type FileProfile = {
    id: string;
    name: string;
    qualities: string[];
};

type FileItem = {
    id: string;
    title: string;
    file_name: string | null;
    source_url: string | null;
    streaming_url: string | null;
    status: FileStatus;
    progress: number;
    tags: string[];
    size: number;
    created_at: string | null;
    profiles: FileProfile[];
};

type ProfileOption = {
    id: string;
    name: string;
    qualities: string[];
    is_default: boolean;
};

type Props = {
    currentFolder: FolderItem | null;
    folders: FolderItem[];
    files: FileItem[];
    profiles: ProfileOption[];
};

const statusStyles: Record<FileStatus, string> = {
    uploaded: 'bg-blue-100 text-blue-700 dark:bg-blue-500/20 dark:text-blue-200',
    progress:
        'bg-amber-100 text-amber-700 dark:bg-amber-500/20 dark:text-amber-200',
    success:
        'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-200',
    failed: 'bg-red-100 text-red-700 dark:bg-red-500/20 dark:text-red-200',
};

const statusLabel: Record<FileStatus, string> = {
    uploaded: 'Uploaded',
    progress: 'Progress',
    success: 'Success',
    failed: 'Failed',
};

function formatSize(bytes: number): string {
    if (!bytes) {
        return '—';
    }

    const units = ['B', 'KB', 'MB', 'GB'];
    let size = bytes;
    let unit = 0;
    while (size >= 1024 && unit < units.length - 1) {
        size /= 1024;
        unit++;
    }
    return `${size.toFixed(size >= 10 || unit === 0 ? 0 : 1)} ${units[unit]}`;
}

const CHUNK_SIZE = 5 * 1024 * 1024;

function xsrfTokenFromCookie(): string {
    const match = document.cookie
        .split('; ')
        .find((row) => row.startsWith('XSRF-TOKEN='));
    return match ? decodeURIComponent(match.split('=')[1]) : '';
}

type UploadState =
    | 'idle'
    | 'uploading'
    | 'paused'
    | 'error'
    | 'finalizing'
    | 'done';

export default function Manage({
    currentFolder,
    folders,
    files,
    profiles,
}: Props) {
    const defaultProfileId =
        profiles.find((profile) => profile.is_default)?.id ??
        profiles[0]?.id ??
        '';

    const [folderDialogOpen, setFolderDialogOpen] = useState(false);
    const [uploadDialogOpen, setUploadDialogOpen] = useState(false);
    const [fileToDelete, setFileToDelete] = useState<FileItem | null>(null);
    const [fileDetails, setFileDetails] = useState<FileItem | null>(null);
    const [uploadTab, setUploadTab] = useState<'file' | 'url'>('file');
    const [tags, setTags] = useState<string[]>([]);
    const [tagInput, setTagInput] = useState('');
    const [selectedFile, setSelectedFile] = useState<File | null>(null);
    const [isDragging, setIsDragging] = useState(false);
    const [uploadTitle, setUploadTitle] = useState('');
    const [selectedProfileId, setSelectedProfileId] =
        useState<string>(defaultProfileId);
    const [uploadErrors, setUploadErrors] = useState<Record<string, string>>(
        {},
    );
    const fileInputRef = useRef<HTMLInputElement>(null);
    const hasProfiles = profiles.length > 0;

    const [uploadState, setUploadState] = useState<UploadState>('idle');
    const [uploadProgress, setUploadProgress] = useState(0);
    const [uploadError, setUploadError] = useState<string | null>(null);
    const uploadIdRef = useRef<string | null>(null);
    const uploadKeyRef = useRef<string | null>(null);
    const nextPartRef = useRef(0);
    const totalPartsRef = useRef(0);
    const etagsRef = useRef<{ part_number: number; etag: string }[]>([]);
    const abortRef = useRef(false);

    setLayoutProps({
        breadcrumbs: currentFolder
            ? [
                  { title: 'Manage', href: manage().url },
                  {
                      title: currentFolder.name,
                      href: `${manage().url}?folder=${currentFolder.id}`,
                  },
              ]
            : [{ title: 'Manage', href: manage().url }],
    });

    const addTag = (raw: string) => {
        const value = raw.trim();
        if (!value) {
            return;
        }
        setTags((prev) => (prev.includes(value) ? prev : [...prev, value]));
        setTagInput('');
    };

    const handleTagKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
        if (event.key === 'Enter' || event.key === ',') {
            event.preventDefault();
            addTag(tagInput);
        } else if (event.key === 'Backspace' && !tagInput && tags.length > 0) {
            setTags((prev) => prev.slice(0, -1));
        }
    };

    const resetUploadForm = () => {
        setTags([]);
        setTagInput('');
        setSelectedFile(null);
        setUploadTitle('');
        setSelectedProfileId(defaultProfileId);
        setUploadErrors({});
        setUploadTab('file');
        setUploadState('idle');
        setUploadProgress(0);
        setUploadError(null);
        uploadIdRef.current = null;
        uploadKeyRef.current = null;
        nextPartRef.current = 0;
        totalPartsRef.current = 0;
        etagsRef.current = [];
        abortRef.current = false;
        if (fileInputRef.current) {
            fileInputRef.current.value = '';
        }
    };

    const postJson = async <T,>(url: string, body: unknown): Promise<T> => {
        const res = await fetch(url, {
            method: 'POST',
            credentials: 'same-origin',
            headers: {
                'Content-Type': 'application/json',
                Accept: 'application/json',
                'X-XSRF-TOKEN': xsrfTokenFromCookie(),
                'X-Requested-With': 'XMLHttpRequest',
            },
            body: JSON.stringify(body),
        });
        if (!res.ok) {
            throw new Error(`${url} failed (${res.status})`);
        }
        return (await res.json()) as T;
    };

    const putPartToS3 = (url: string, slice: Blob): Promise<string> =>
        new Promise((resolve, reject) => {
            const xhr = new XMLHttpRequest();
            xhr.open('PUT', url);
            xhr.onload = () => {
                if (xhr.status >= 200 && xhr.status < 300) {
                    const etag = xhr.getResponseHeader('ETag');
                    if (!etag) {
                        reject(
                            new Error(
                                'S3 response missing ETag (check bucket CORS ExposeHeaders).',
                            ),
                        );
                        return;
                    }
                    resolve(etag);
                } else {
                    reject(new Error(`S3 PUT failed (${xhr.status})`));
                }
            };
            xhr.onerror = () => reject(new Error('Network error during PUT'));
            xhr.onabort = () => reject(new Error('Upload aborted'));
            xhr.send(slice);
        });

    const cancelMultipartUpload = async () => {
        if (!uploadIdRef.current) {
            return;
        }
        try {
            await postJson('/manage/files/multipart/abort', {
                upload_id: uploadIdRef.current,
            });
        } catch {
            /* ignore */
        }
    };

    const runMultipartLoop = async () => {
        if (!selectedFile) {
            return;
        }

        setUploadState('uploading');
        setUploadError(null);
        abortRef.current = false;

        if (!uploadIdRef.current) {
            try {
                const init = await postJson<{
                    upload_id: string;
                    key: string;
                }>('/manage/files/multipart/init', {
                    file_name: selectedFile.name,
                    folder_id: currentFolder?.id ?? null,
                    profile_id: selectedProfileId,
                });
                uploadIdRef.current = init.upload_id;
                uploadKeyRef.current = init.key;
            } catch (err) {
                setUploadState('error');
                setUploadError(
                    err instanceof Error ? err.message : 'Init failed.',
                );
                return;
            }
        }

        const total = totalPartsRef.current;

        while (nextPartRef.current < total) {
            if (abortRef.current) {
                return;
            }
            const index = nextPartRef.current;
            const partNumber = index + 1;
            const start = index * CHUNK_SIZE;
            const end = Math.min(start + CHUNK_SIZE, selectedFile.size);
            const slice = selectedFile.slice(start, end);

            try {
                const { url } = await postJson<{ url: string }>(
                    '/manage/files/multipart/sign-part',
                    {
                        upload_id: uploadIdRef.current,
                        part_number: partNumber,
                    },
                );
                const etag = await putPartToS3(url, slice);
                etagsRef.current.push({ part_number: partNumber, etag });
            } catch (err) {
                setUploadState('error');
                setUploadError(
                    err instanceof Error ? err.message : 'Upload failed.',
                );
                return;
            }

            nextPartRef.current = index + 1;
            setUploadProgress(Math.round((nextPartRef.current / total) * 100));
        }

        setUploadState('finalizing');
        try {
            await postJson('/manage/files/multipart/complete', {
                upload_id: uploadIdRef.current,
                title: uploadTitle,
                parts: etagsRef.current,
                tags,
                folder_id: currentFolder?.id ?? null,
            });
        } catch (err) {
            setUploadState('error');
            setUploadError(
                err instanceof Error ? err.message : 'Finalize failed.',
            );
            return;
        }

        setUploadState('done');
        setUploadDialogOpen(false);
        resetUploadForm();
        router.reload({ only: ['files', 'folders'] });
    };

    const startChunkedUpload = () => {
        if (!selectedFile) {
            setUploadErrors({ file: 'Please choose a video file.' });
            return;
        }
        if (!uploadTitle.trim()) {
            setUploadErrors({ title: 'Title is required.' });
            return;
        }
        if (!selectedProfileId) {
            setUploadErrors({ profile_id: 'Please select a profile.' });
            return;
        }
        uploadIdRef.current = null;
        uploadKeyRef.current = null;
        nextPartRef.current = 0;
        etagsRef.current = [];
        totalPartsRef.current = Math.max(
            1,
            Math.ceil(selectedFile.size / CHUNK_SIZE),
        );
        setUploadProgress(0);
        setUploadErrors({});
        void runMultipartLoop();
    };

    const resumeChunkedUpload = () => {
        void runMultipartLoop();
    };

    const pickFile = (file: File | null) => {
        if (!file) {
            return;
        }
        const allowed = [
            'video/mp4',
            'video/quicktime',
            'application/octet-stream',
        ];
        const lowerName = file.name.toLowerCase();
        const extOk = lowerName.endsWith('.mp4') || lowerName.endsWith('.mov');
        if (!allowed.includes(file.type) && !extOk) {
            setUploadErrors({ file: 'Only .mp4 and .mov files are allowed.' });
            return;
        }
        setSelectedFile(file);
        setUploadErrors((prev) => ({ ...prev, file: '' }));
        if (!uploadTitle) {
            setUploadTitle(file.name.replace(/\.[^.]+$/, ''));
        }
    };

    const handleFileInputChange = (event: ChangeEvent<HTMLInputElement>) => {
        pickFile(event.target.files?.[0] ?? null);
    };

    const handleDrop = (event: DragEvent<HTMLDivElement>) => {
        event.preventDefault();
        setIsDragging(false);
        pickFile(event.dataTransfer.files?.[0] ?? null);
    };

    const handleUploadFileSubmit = (event: FormEvent<HTMLFormElement>) => {
        event.preventDefault();
        startChunkedUpload();
    };

    return (
        <>
            <Head title="Manage" />

            <div className="flex h-full flex-1 flex-col gap-6 p-4">
                <Heading
                    variant="page"
                    title="Manage"
                    description={
                        currentFolder
                            ? `Viewing folder “${currentFolder.name}”`
                            : 'Create folders and upload videos (mp4, mov).'
                    }
                />

                <div className="flex items-center justify-between gap-2">
                    <div className="flex items-center gap-1 text-sm text-muted-foreground">
                        <Link
                            href={manage().url}
                            className="rounded px-1.5 py-1 hover:bg-muted hover:text-foreground"
                        >
                            Root
                        </Link>
                        {currentFolder && (
                            <>
                                <ChevronRight className="size-3.5" />
                                <span className="rounded px-1.5 py-1 text-foreground">
                                    {currentFolder.name}
                                </span>
                            </>
                        )}
                    </div>

                    <div className="flex items-center gap-2">
                        <Dialog
                            open={folderDialogOpen}
                            onOpenChange={setFolderDialogOpen}
                        >
                            <DialogTrigger asChild>
                                <Button variant="outline">
                                    <Plus className="size-3.5" />
                                    New folder
                                </Button>
                            </DialogTrigger>
                            <DialogContent>
                                <DialogHeader>
                                    <DialogTitle>New folder</DialogTitle>
                                    <DialogDescription>
                                        Folders help organize uploaded videos.
                                    </DialogDescription>
                                </DialogHeader>

                                <Form
                                    {...ManageController.storeFolder.form()}
                                    options={{ preserveScroll: true }}
                                    onSuccess={() => setFolderDialogOpen(false)}
                                    resetOnSuccess
                                    className="space-y-4"
                                >
                                    {({ processing, errors }) => (
                                        <>
                                            <div className="grid gap-2">
                                                <Label htmlFor="folder-name">
                                                    Name
                                                </Label>
                                                <Input
                                                    id="folder-name"
                                                    name="name"
                                                    autoFocus
                                                    placeholder="e.g. Marketing"
                                                />
                                                <InputError
                                                    message={errors.name}
                                                />
                                            </div>
                                            <DialogFooter className="gap-2">
                                                <DialogClose asChild>
                                                    <Button variant="secondary">
                                                        Cancel
                                                    </Button>
                                                </DialogClose>
                                                <Button
                                                    type="submit"
                                                    disabled={processing}
                                                >
                                                    Create
                                                </Button>
                                            </DialogFooter>
                                        </>
                                    )}
                                </Form>
                            </DialogContent>
                        </Dialog>

                        <Dialog
                            open={uploadDialogOpen}
                            onOpenChange={(open) => {
                                setUploadDialogOpen(open);
                                if (!open) {
                                    resetUploadForm();
                                }
                            }}
                        >
                            <DialogTrigger asChild>
                                <Button>
                                    <Upload className="size-3.5" />
                                    Add video
                                </Button>
                            </DialogTrigger>
                            <DialogContent>
                                <DialogHeader>
                                    <DialogTitle>Add video</DialogTitle>
                                    <DialogDescription>
                                        Upload a file from your device, or
                                        import one from a URL.
                                    </DialogDescription>
                                </DialogHeader>

                                <div className="inline-flex w-full rounded-lg border bg-muted p-1 text-sm">
                                    <button
                                        type="button"
                                        onClick={() => setUploadTab('file')}
                                        className={`flex flex-1 items-center justify-center gap-1.5 rounded-md px-3 py-1.5 transition ${
                                            uploadTab === 'file'
                                                ? 'bg-background shadow-sm'
                                                : 'text-muted-foreground hover:text-foreground'
                                        }`}
                                    >
                                        <UploadCloud className="size-3.5" />
                                        Upload file
                                    </button>
                                    <button
                                        type="button"
                                        onClick={() => setUploadTab('url')}
                                        className={`flex flex-1 items-center justify-center gap-1.5 rounded-md px-3 py-1.5 transition ${
                                            uploadTab === 'url'
                                                ? 'bg-background shadow-sm'
                                                : 'text-muted-foreground hover:text-foreground'
                                        }`}
                                    >
                                        <LinkIcon className="size-3.5" />
                                        From URL
                                    </button>
                                </div>

                                {uploadTab === 'file' && (
                                    <form
                                        onSubmit={handleUploadFileSubmit}
                                        className="space-y-4"
                                    >
                                        <div className="grid gap-2">
                                            <Label htmlFor="file-title">
                                                Title
                                            </Label>
                                            <Input
                                                id="file-title"
                                                value={uploadTitle}
                                                onChange={(e) =>
                                                    setUploadTitle(
                                                        e.target.value,
                                                    )
                                                }
                                                placeholder="Video title"
                                            />
                                            <InputError
                                                message={uploadErrors.title}
                                            />
                                        </div>

                                        <div className="grid gap-2">
                                            <Label htmlFor="file-profile">
                                                Encoding profile
                                            </Label>
                                            {hasProfiles ? (
                                                <Select
                                                    value={selectedProfileId}
                                                    onValueChange={
                                                        setSelectedProfileId
                                                    }
                                                >
                                                    <SelectTrigger
                                                        id="file-profile"
                                                        className="w-full"
                                                    >
                                                        <SelectValue placeholder="Select a profile" />
                                                    </SelectTrigger>
                                                    <SelectContent>
                                                        {profiles.map(
                                                            (profile) => (
                                                                <SelectItem
                                                                    key={
                                                                        profile.id
                                                                    }
                                                                    value={
                                                                        profile.id
                                                                    }
                                                                >
                                                                    <span>
                                                                        {
                                                                            profile.name
                                                                        }
                                                                    </span>
                                                                    {profile.is_default && (
                                                                        <Badge
                                                                            variant="secondary"
                                                                            className="ml-2"
                                                                        >
                                                                            Default
                                                                        </Badge>
                                                                    )}
                                                                </SelectItem>
                                                            ),
                                                        )}
                                                    </SelectContent>
                                                </Select>
                                            ) : (
                                                <p className="rounded-md border border-dashed px-3 py-2 text-xs text-muted-foreground">
                                                    No encoding profiles yet.
                                                    Ask an admin to create one
                                                    before uploading.
                                                </p>
                                            )}
                                            <InputError
                                                message={
                                                    uploadErrors.profile_id
                                                }
                                            />
                                        </div>

                                        <div className="grid gap-2">
                                            <Label>File</Label>
                                            <div
                                                role="button"
                                                tabIndex={0}
                                                onClick={() =>
                                                    fileInputRef.current?.click()
                                                }
                                                onKeyDown={(e) => {
                                                    if (
                                                        e.key === 'Enter' ||
                                                        e.key === ' '
                                                    ) {
                                                        e.preventDefault();
                                                        fileInputRef.current?.click();
                                                    }
                                                }}
                                                onDragOver={(e) => {
                                                    e.preventDefault();
                                                    setIsDragging(true);
                                                }}
                                                onDragLeave={() =>
                                                    setIsDragging(false)
                                                }
                                                onDrop={handleDrop}
                                                className={`flex cursor-pointer flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed px-4 py-8 text-center transition ${
                                                    isDragging
                                                        ? 'border-primary bg-primary/5'
                                                        : 'border-muted-foreground/25 hover:border-muted-foreground/50'
                                                }`}
                                            >
                                                <UploadCloud className="size-6 text-muted-foreground" />
                                                {selectedFile ? (
                                                    <div className="text-sm">
                                                        <p className="font-medium">
                                                            {selectedFile.name}
                                                        </p>
                                                        <p className="text-xs text-muted-foreground">
                                                            {formatSize(
                                                                selectedFile.size,
                                                            )}
                                                        </p>
                                                    </div>
                                                ) : (
                                                    <div className="text-sm text-muted-foreground">
                                                        <p>
                                                            Click to select or
                                                            drag & drop
                                                        </p>
                                                        <p className="text-xs">
                                                            .mp4 or .mov
                                                        </p>
                                                    </div>
                                                )}
                                                <input
                                                    ref={fileInputRef}
                                                    type="file"
                                                    accept="video/mp4,video/quicktime,.mp4,.mov"
                                                    onChange={
                                                        handleFileInputChange
                                                    }
                                                    className="hidden"
                                                />
                                            </div>
                                            <InputError
                                                message={uploadErrors.file}
                                            />
                                        </div>

                                        <div className="grid gap-2">
                                            <Label htmlFor="file-tags">
                                                Tags
                                            </Label>
                                            <div className="flex flex-wrap items-center gap-2 rounded-md border bg-transparent px-2 py-1.5 focus-within:ring-1 focus-within:ring-ring">
                                                {tags.map((tag) => (
                                                    <Badge
                                                        key={tag}
                                                        variant="secondary"
                                                        className="gap-1"
                                                    >
                                                        {tag}
                                                        <button
                                                            type="button"
                                                            onClick={() =>
                                                                setTags(
                                                                    (prev) =>
                                                                        prev.filter(
                                                                            (
                                                                                t,
                                                                            ) =>
                                                                                t !==
                                                                                tag,
                                                                        ),
                                                                )
                                                            }
                                                            className="text-muted-foreground hover:text-foreground"
                                                            aria-label={`Remove ${tag}`}
                                                        >
                                                            <X className="size-3" />
                                                        </button>
                                                    </Badge>
                                                ))}
                                                <input
                                                    id="file-tags"
                                                    value={tagInput}
                                                    onChange={(e) =>
                                                        setTagInput(
                                                            e.target.value,
                                                        )
                                                    }
                                                    onKeyDown={handleTagKeyDown}
                                                    onBlur={() =>
                                                        addTag(tagInput)
                                                    }
                                                    placeholder={
                                                        tags.length === 0
                                                            ? 'Press enter to add'
                                                            : ''
                                                    }
                                                    className="flex-1 min-w-[8ch] bg-transparent text-sm outline-none"
                                                />
                                            </div>
                                        </div>

                                        {(uploadState === 'uploading' ||
                                            uploadState === 'finalizing' ||
                                            uploadState === 'error') && (
                                            <div className="space-y-2 rounded-md border bg-muted/40 p-3">
                                                <div className="flex items-center justify-between text-xs text-muted-foreground">
                                                    <span>
                                                        {uploadState ===
                                                        'finalizing'
                                                            ? 'Finalizing…'
                                                            : uploadState ===
                                                                'error'
                                                              ? 'Upload paused'
                                                              : `Uploading part ${nextPartRef.current + 1}/${totalPartsRef.current}`}
                                                    </span>
                                                    <span className="font-medium">
                                                        {uploadProgress}%
                                                    </span>
                                                </div>
                                                <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
                                                    <div
                                                        className={`h-full transition-all ${
                                                            uploadState ===
                                                            'error'
                                                                ? 'bg-red-500'
                                                                : 'bg-primary'
                                                        }`}
                                                        style={{
                                                            width: `${uploadProgress}%`,
                                                        }}
                                                    />
                                                </div>
                                                {uploadError && (
                                                    <p className="text-xs text-red-600">
                                                        {uploadError}
                                                    </p>
                                                )}
                                            </div>
                                        )}

                                        <DialogFooter className="gap-2">
                                            {uploadState === 'error' ? (
                                                <>
                                                    <Button
                                                        type="button"
                                                        variant="secondary"
                                                        onClick={async () => {
                                                            abortRef.current =
                                                                true;
                                                            await cancelMultipartUpload();
                                                            setUploadDialogOpen(
                                                                false,
                                                            );
                                                            resetUploadForm();
                                                        }}
                                                    >
                                                        Cancel
                                                    </Button>
                                                    <Button
                                                        type="button"
                                                        onClick={
                                                            resumeChunkedUpload
                                                        }
                                                    >
                                                        Resume upload
                                                    </Button>
                                                </>
                                            ) : (
                                                <>
                                                    <DialogClose asChild>
                                                        <Button
                                                            variant="secondary"
                                                            disabled={
                                                                uploadState ===
                                                                    'uploading' ||
                                                                uploadState ===
                                                                    'finalizing'
                                                            }
                                                        >
                                                            Cancel
                                                        </Button>
                                                    </DialogClose>
                                                    <Button
                                                        type="submit"
                                                        disabled={
                                                            !hasProfiles ||
                                                            uploadState ===
                                                                'uploading' ||
                                                            uploadState ===
                                                                'finalizing'
                                                        }
                                                    >
                                                        {uploadState ===
                                                        'uploading'
                                                            ? 'Uploading…'
                                                            : uploadState ===
                                                                'finalizing'
                                                              ? 'Finalizing…'
                                                              : 'Upload'}
                                                    </Button>
                                                </>
                                            )}
                                        </DialogFooter>
                                    </form>
                                )}

                                {uploadTab === 'url' && (
                                    <Form
                                        {...ManageController.storeFromUrl.form()}
                                        options={{ preserveScroll: true }}
                                        onSuccess={() => {
                                            setUploadDialogOpen(false);
                                            resetUploadForm();
                                        }}
                                        resetOnSuccess
                                        className="space-y-4"
                                    >
                                        {({ processing, errors }) => (
                                            <>
                                                <input
                                                    type="hidden"
                                                    name="folder_id"
                                                    value={
                                                        currentFolder?.id ?? ''
                                                    }
                                                />
                                                <input
                                                    type="hidden"
                                                    name="tags"
                                                    value={JSON.stringify(tags)}
                                                />
                                                <input
                                                    type="hidden"
                                                    name="profile_id"
                                                    value={selectedProfileId}
                                                />

                                                <div className="grid gap-2">
                                                    <Label htmlFor="url-title">
                                                        Title
                                                    </Label>
                                                    <Input
                                                        id="url-title"
                                                        name="title"
                                                        placeholder="Video title"
                                                    />
                                                    <InputError
                                                        message={errors.title}
                                                    />
                                                </div>

                                                <div className="grid gap-2">
                                                    <Label htmlFor="url-profile">
                                                        Encoding profile
                                                    </Label>
                                                    {hasProfiles ? (
                                                        <Select
                                                            value={
                                                                selectedProfileId
                                                            }
                                                            onValueChange={
                                                                setSelectedProfileId
                                                            }
                                                        >
                                                            <SelectTrigger
                                                                id="url-profile"
                                                                className="w-full"
                                                            >
                                                                <SelectValue placeholder="Select a profile" />
                                                            </SelectTrigger>
                                                            <SelectContent>
                                                                {profiles.map(
                                                                    (
                                                                        profile,
                                                                    ) => (
                                                                        <SelectItem
                                                                            key={
                                                                                profile.id
                                                                            }
                                                                            value={
                                                                                profile.id
                                                                            }
                                                                        >
                                                                            <span>
                                                                                {
                                                                                    profile.name
                                                                                }
                                                                            </span>
                                                                            {profile.is_default && (
                                                                                <Badge
                                                                                    variant="secondary"
                                                                                    className="ml-2"
                                                                                >
                                                                                    Default
                                                                                </Badge>
                                                                            )}
                                                                        </SelectItem>
                                                                    ),
                                                                )}
                                                            </SelectContent>
                                                        </Select>
                                                    ) : (
                                                        <p className="rounded-md border border-dashed px-3 py-2 text-xs text-muted-foreground">
                                                            No encoding profiles
                                                            yet. Ask an admin to
                                                            create one.
                                                        </p>
                                                    )}
                                                    <InputError
                                                        message={
                                                            errors.profile_id
                                                        }
                                                    />
                                                </div>

                                                <div className="grid gap-2">
                                                    <Label htmlFor="source-url">
                                                        Source URL
                                                    </Label>
                                                    <Input
                                                        id="source-url"
                                                        name="source_url"
                                                        type="url"
                                                        placeholder="https://example.com/video.mp4"
                                                    />
                                                    <InputError
                                                        message={
                                                            errors.source_url
                                                        }
                                                    />
                                                    <p className="text-xs text-muted-foreground">
                                                        The worker will
                                                        download and transcode
                                                        this video.
                                                    </p>
                                                </div>

                                                <div className="grid gap-2">
                                                    <Label htmlFor="url-tags">
                                                        Tags
                                                    </Label>
                                                    <div className="flex flex-wrap items-center gap-2 rounded-md border bg-transparent px-2 py-1.5 focus-within:ring-1 focus-within:ring-ring">
                                                        {tags.map((tag) => (
                                                            <Badge
                                                                key={tag}
                                                                variant="secondary"
                                                                className="gap-1"
                                                            >
                                                                {tag}
                                                                <button
                                                                    type="button"
                                                                    onClick={() =>
                                                                        setTags(
                                                                            (
                                                                                prev,
                                                                            ) =>
                                                                                prev.filter(
                                                                                    (
                                                                                        t,
                                                                                    ) =>
                                                                                        t !==
                                                                                        tag,
                                                                                ),
                                                                        )
                                                                    }
                                                                    className="text-muted-foreground hover:text-foreground"
                                                                    aria-label={`Remove ${tag}`}
                                                                >
                                                                    <X className="size-3" />
                                                                </button>
                                                            </Badge>
                                                        ))}
                                                        <input
                                                            id="url-tags"
                                                            value={tagInput}
                                                            onChange={(e) =>
                                                                setTagInput(
                                                                    e.target
                                                                        .value,
                                                                )
                                                            }
                                                            onKeyDown={
                                                                handleTagKeyDown
                                                            }
                                                            onBlur={() =>
                                                                addTag(tagInput)
                                                            }
                                                            placeholder={
                                                                tags.length ===
                                                                0
                                                                    ? 'Press enter to add'
                                                                    : ''
                                                            }
                                                            className="flex-1 min-w-[8ch] bg-transparent text-sm outline-none"
                                                        />
                                                    </div>
                                                </div>

                                                <DialogFooter className="gap-2">
                                                    <DialogClose asChild>
                                                        <Button variant="secondary">
                                                            Cancel
                                                        </Button>
                                                    </DialogClose>
                                                    <Button
                                                        type="submit"
                                                        disabled={
                                                            processing ||
                                                            !hasProfiles
                                                        }
                                                    >
                                                        Add from URL
                                                    </Button>
                                                </DialogFooter>
                                            </>
                                        )}
                                    </Form>
                                )}
                            </DialogContent>
                        </Dialog>
                    </div>
                </div>

                {!currentFolder && folders.length > 0 && (
                    <div>
                        <h2 className="mb-2 text-sm font-medium text-muted-foreground">
                            Folders
                        </h2>
                        <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6">
                            {folders.map((folder) => (
                                <Link
                                    key={folder.id}
                                    href={`${manage().url}?folder=${folder.id}`}
                                    className="group flex items-center gap-3 rounded-lg border bg-card p-3 transition hover:border-foreground/40 hover:shadow-sm"
                                >
                                    <div className="flex size-9 items-center justify-center rounded-md bg-muted text-muted-foreground group-hover:text-foreground">
                                        <FolderIcon className="size-4" />
                                    </div>
                                    <span className="truncate text-sm font-medium">
                                        {folder.name}
                                    </span>
                                </Link>
                            ))}
                        </div>
                    </div>
                )}

                <div>
                    <h2 className="mb-2 text-sm font-medium text-muted-foreground">
                        Files
                    </h2>
                    <div className="rounded-lg border">
                        <Table>
                            <TableHeader>
                                <TableRow>
                                    <TableHead>Title</TableHead>
                                    <TableHead>File name</TableHead>
                                    <TableHead>Status</TableHead>
                                    <TableHead>Profiles</TableHead>
                                    <TableHead>Tags</TableHead>
                                    <TableHead className="w-20">Size</TableHead>
                                    <TableHead className="w-10">
                                        <span className="sr-only">Actions</span>
                                    </TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {files.map((file) => (
                                    <TableRow
                                        key={file.id}
                                        onClick={() => setFileDetails(file)}
                                        className="cursor-pointer"
                                    >
                                        <TableCell className="font-medium">
                                            {file.title}
                                        </TableCell>
                                        <TableCell className="max-w-[240px] truncate text-muted-foreground">
                                            {file.file_name ??
                                                file.source_url ??
                                                '—'}
                                        </TableCell>
                                        <TableCell>
                                            <div className="flex flex-col gap-1">
                                                <Badge
                                                    variant="secondary"
                                                    className={
                                                        statusStyles[file.status]
                                                    }
                                                >
                                                    {statusLabel[file.status]}
                                                    {file.status ===
                                                        'progress' &&
                                                        ` · ${file.progress}%`}
                                                </Badge>
                                                {file.status === 'progress' && (
                                                    <div className="h-1 w-20 overflow-hidden rounded-full bg-muted">
                                                        <div
                                                            className="h-full bg-primary transition-all"
                                                            style={{
                                                                width: `${file.progress}%`,
                                                            }}
                                                        />
                                                    </div>
                                                )}
                                            </div>
                                        </TableCell>
                                        <TableCell>
                                            <div className="flex flex-wrap gap-1">
                                                {file.profiles.length === 0 ? (
                                                    <span className="text-xs text-muted-foreground">
                                                        —
                                                    </span>
                                                ) : (
                                                    file.profiles.map(
                                                        (profile) => (
                                                            <Badge
                                                                key={profile.id}
                                                                variant="secondary"
                                                                title={profile.qualities.join(
                                                                    ', ',
                                                                )}
                                                            >
                                                                {profile.name}
                                                            </Badge>
                                                        ),
                                                    )
                                                )}
                                            </div>
                                        </TableCell>
                                        <TableCell>
                                            <div className="flex flex-wrap gap-1">
                                                {file.tags.length === 0 ? (
                                                    <span className="text-xs text-muted-foreground">
                                                        —
                                                    </span>
                                                ) : (
                                                    file.tags.map((tag) => (
                                                        <Badge
                                                            key={tag}
                                                            variant="outline"
                                                        >
                                                            {tag}
                                                        </Badge>
                                                    ))
                                                )}
                                            </div>
                                        </TableCell>
                                        <TableCell className="text-muted-foreground">
                                            {formatSize(file.size)}
                                        </TableCell>
                                        <TableCell
                                            onClick={(event) =>
                                                event.stopPropagation()
                                            }
                                        >
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
                                                    <DropdownMenuItem
                                                        disabled={
                                                            !file.streaming_url
                                                        }
                                                        onClick={() => {
                                                            if (
                                                                file.streaming_url
                                                            ) {
                                                                navigator.clipboard.writeText(
                                                                    file.streaming_url,
                                                                );
                                                            }
                                                        }}
                                                    >
                                                        Copy streaming URL
                                                    </DropdownMenuItem>
                                                    <DropdownMenuItem disabled>
                                                        Requeue (soon)
                                                    </DropdownMenuItem>
                                                    <DropdownMenuSeparator />
                                                    <DropdownMenuItem
                                                        variant="destructive"
                                                        disabled={
                                                            file.status ===
                                                            'progress'
                                                        }
                                                        onClick={() =>
                                                            setFileToDelete(
                                                                file,
                                                            )
                                                        }
                                                    >
                                                        Delete
                                                    </DropdownMenuItem>
                                                </DropdownMenuContent>
                                            </DropdownMenu>
                                        </TableCell>
                                    </TableRow>
                                ))}

                                {files.length === 0 && (
                                    <TableRow>
                                        <TableCell
                                            colSpan={7}
                                            className="h-24 text-center text-sm text-muted-foreground"
                                        >
                                            No files yet. Upload a video to get
                                            started.
                                        </TableCell>
                                    </TableRow>
                                )}
                            </TableBody>
                        </Table>
                    </div>
                </div>
            </div>

            <Dialog
                open={fileDetails !== null}
                onOpenChange={(open) => {
                    if (!open) {
                        setFileDetails(null);
                    }
                }}
            >
                <DialogContent className="sm:max-w-lg">
                    <DialogHeader>
                        <DialogTitle>
                            {fileDetails?.title ?? 'Media'}
                        </DialogTitle>
                        <DialogDescription>
                            {fileDetails?.file_name ??
                                fileDetails?.source_url ??
                                '—'}
                        </DialogDescription>
                    </DialogHeader>

                    {fileDetails && (
                        <div className="space-y-4">
                            <div className="grid grid-cols-[8rem_1fr] gap-x-4 gap-y-2">
                                <span className="text-xs text-muted-foreground">
                                    Status
                                </span>
                                <div className="space-y-1.5">
                                    <Badge
                                        variant="secondary"
                                        className={statusStyles[fileDetails.status]}
                                    >
                                        {statusLabel[fileDetails.status]}
                                        {fileDetails.status === 'progress' &&
                                            ` · ${fileDetails.progress}%`}
                                    </Badge>
                                    {fileDetails.status === 'progress' && (
                                        <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
                                            <div
                                                className="h-full bg-primary transition-all"
                                                style={{
                                                    width: `${fileDetails.progress}%`,
                                                }}
                                            />
                                        </div>
                                    )}
                                </div>

                                <span className="text-xs text-muted-foreground">
                                    Size
                                </span>
                                <span className="text-xs">
                                    {formatSize(fileDetails.size)}
                                </span>

                                <span className="text-xs text-muted-foreground">
                                    Tags
                                </span>
                                <div className="flex flex-wrap gap-1">
                                    {fileDetails.tags.length === 0 ? (
                                        <span className="text-xs text-muted-foreground">
                                            —
                                        </span>
                                    ) : (
                                        fileDetails.tags.map((tag) => (
                                            <Badge
                                                key={tag}
                                                variant="outline"
                                            >
                                                {tag}
                                            </Badge>
                                        ))
                                    )}
                                </div>

                                <span className="text-xs text-muted-foreground">
                                    Streaming URL
                                </span>
                                <div className="min-w-0">
                                    {fileDetails.streaming_url ? (
                                        <div className="flex items-center gap-2">
                                            <a
                                                href={fileDetails.streaming_url}
                                                target="_blank"
                                                rel="noopener noreferrer"
                                                className="truncate text-xs text-primary underline underline-offset-2"
                                            >
                                                {fileDetails.streaming_url}
                                            </a>
                                            <Button
                                                type="button"
                                                variant="ghost"
                                                size="icon-sm"
                                                aria-label="Copy streaming URL"
                                                onClick={() =>
                                                    navigator.clipboard.writeText(
                                                        fileDetails.streaming_url!,
                                                    )
                                                }
                                            >
                                                <LinkIcon className="size-3.5" />
                                            </Button>
                                        </div>
                                    ) : (
                                        <span className="text-xs text-muted-foreground">
                                            Not ready yet
                                        </span>
                                    )}
                                </div>
                            </div>

                            <div className="space-y-2">
                                <h3 className="text-xs font-medium text-muted-foreground">
                                    Transcode profiles
                                </h3>
                                {fileDetails.profiles.length === 0 ? (
                                    <p className="rounded-md border border-dashed px-3 py-2 text-xs text-muted-foreground">
                                        No profile recorded for this upload.
                                    </p>
                                ) : (
                                    <div className="space-y-2">
                                        {fileDetails.profiles.map((profile) => (
                                            <div
                                                key={profile.id}
                                                className="rounded-md border bg-card px-3 py-2"
                                            >
                                                <div className="flex items-center justify-between gap-2">
                                                    <span className="text-xs font-medium">
                                                        {profile.name}
                                                    </span>
                                                    <span className="text-[10px] text-muted-foreground">
                                                        {profile.qualities.length}{' '}
                                                        rendition
                                                        {profile.qualities
                                                            .length === 1
                                                            ? ''
                                                            : 's'}
                                                    </span>
                                                </div>
                                                <div className="mt-2 flex flex-wrap gap-1">
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
                                            </div>
                                        ))}
                                    </div>
                                )}
                            </div>
                        </div>
                    )}

                    <DialogFooter>
                        <DialogClose asChild>
                            <Button variant="secondary">Close</Button>
                        </DialogClose>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            <Dialog
                open={fileToDelete !== null}
                onOpenChange={(open) => {
                    if (!open) {
                        setFileToDelete(null);
                    }
                }}
            >
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Delete file</DialogTitle>
                        <DialogDescription>
                            This permanently deletes “{fileToDelete?.title}”
                            and its file on S3. This cannot be undone.
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter className="gap-2">
                        <DialogClose asChild>
                            <Button variant="secondary">Cancel</Button>
                        </DialogClose>
                        <Button
                            variant="destructive"
                            onClick={() => {
                                if (!fileToDelete) {
                                    return;
                                }
                                router.delete(
                                    `/manage/files/${fileToDelete.id}`,
                                    {
                                        preserveScroll: true,
                                        onFinish: () => setFileToDelete(null),
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
