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
    DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
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
import { manage } from '@/routes';

type FolderItem = {
    id: string;
    name: string;
};

type FileStatus = 'uploaded' | 'progress' | 'success' | 'failed';

type FileItem = {
    id: string;
    title: string;
    file_name: string | null;
    source_url: string | null;
    streaming_url: string | null;
    status: FileStatus;
    tags: string[];
    size: number;
    created_at: string | null;
};

type Props = {
    currentFolder: FolderItem | null;
    folders: FolderItem[];
    files: FileItem[];
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

export default function Manage({ currentFolder, folders, files }: Props) {
    const [folderDialogOpen, setFolderDialogOpen] = useState(false);
    const [uploadDialogOpen, setUploadDialogOpen] = useState(false);
    const [uploadTab, setUploadTab] = useState<'file' | 'url'>('file');
    const [tags, setTags] = useState<string[]>([]);
    const [tagInput, setTagInput] = useState('');
    const [selectedFile, setSelectedFile] = useState<File | null>(null);
    const [isDragging, setIsDragging] = useState(false);
    const [uploadTitle, setUploadTitle] = useState('');
    const [uploading, setUploading] = useState(false);
    const [uploadErrors, setUploadErrors] = useState<Record<string, string>>(
        {},
    );
    const fileInputRef = useRef<HTMLInputElement>(null);

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
        setUploadErrors({});
        setUploadTab('file');
        if (fileInputRef.current) {
            fileInputRef.current.value = '';
        }
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
        if (!selectedFile) {
            setUploadErrors({ file: 'Please choose a video file.' });
            return;
        }
        const formData = new FormData();
        formData.append('title', uploadTitle);
        formData.append('file', selectedFile);
        formData.append('tags', JSON.stringify(tags));
        if (currentFolder) {
            formData.append('folder_id', currentFolder.id);
        }
        setUploading(true);
        router.post(ManageController.storeFile.url(), formData, {
            forceFormData: true,
            preserveScroll: true,
            onError: (errors) => {
                setUploadErrors(errors as Record<string, string>);
            },
            onSuccess: () => {
                setUploadDialogOpen(false);
                resetUploadForm();
            },
            onFinish: () => setUploading(false),
        });
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

                                        <DialogFooter className="gap-2">
                                            <DialogClose asChild>
                                                <Button variant="secondary">
                                                    Cancel
                                                </Button>
                                            </DialogClose>
                                            <Button
                                                type="submit"
                                                disabled={uploading}
                                            >
                                                {uploading
                                                    ? 'Uploading…'
                                                    : 'Upload'}
                                            </Button>
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
                                                        disabled={processing}
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
                                    <TableHead>Tags</TableHead>
                                    <TableHead className="w-20">Size</TableHead>
                                    <TableHead className="w-10">
                                        <span className="sr-only">Actions</span>
                                    </TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {files.map((file) => (
                                    <TableRow key={file.id}>
                                        <TableCell className="font-medium">
                                            {file.title}
                                        </TableCell>
                                        <TableCell className="max-w-[240px] truncate text-muted-foreground">
                                            {file.file_name ??
                                                file.source_url ??
                                                '—'}
                                        </TableCell>
                                        <TableCell>
                                            <Badge
                                                variant="secondary"
                                                className={
                                                    statusStyles[file.status]
                                                }
                                            >
                                                {statusLabel[file.status]}
                                            </Badge>
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
                                                </DropdownMenuContent>
                                            </DropdownMenu>
                                        </TableCell>
                                    </TableRow>
                                ))}

                                {files.length === 0 && (
                                    <TableRow>
                                        <TableCell
                                            colSpan={6}
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
        </>
    );
}
