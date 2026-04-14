<?php

namespace App\Services;

use Aws\S3\S3Client;
use Illuminate\Filesystem\FilesystemAdapter;
use Illuminate\Support\Facades\Storage;

class S3MultipartUploadManager
{
    public function __construct(private readonly string $disk = 's3') {}

    public function initiate(string $key, string $contentType): string
    {
        $result = $this->client()->createMultipartUpload([
            'Bucket' => $this->bucket(),
            'Key' => $key,
            'ContentType' => $contentType,
        ]);

        return (string) $result['UploadId'];
    }

    public function presignPart(
        string $key,
        string $uploadId,
        int $partNumber,
        string $expiresAt = '+20 minutes',
    ): string {
        $client = $this->client();
        $command = $client->getCommand('UploadPart', [
            'Bucket' => $this->bucket(),
            'Key' => $key,
            'UploadId' => $uploadId,
            'PartNumber' => $partNumber,
        ]);

        return (string) $client->createPresignedRequest($command, $expiresAt)->getUri();
    }

    /**
     * @param  list<array{PartNumber: int, ETag: string}>  $parts
     */
    public function complete(string $key, string $uploadId, array $parts): void
    {
        $this->client()->completeMultipartUpload([
            'Bucket' => $this->bucket(),
            'Key' => $key,
            'UploadId' => $uploadId,
            'MultipartUpload' => ['Parts' => $parts],
        ]);
    }

    public function abort(string $key, string $uploadId): void
    {
        $this->client()->abortMultipartUpload([
            'Bucket' => $this->bucket(),
            'Key' => $key,
            'UploadId' => $uploadId,
        ]);
    }

    public function size(string $key): int
    {
        $result = $this->client()->headObject([
            'Bucket' => $this->bucket(),
            'Key' => $key,
        ]);

        return (int) ($result['ContentLength'] ?? 0);
    }

    private function client(): S3Client
    {
        /** @var FilesystemAdapter $adapter */
        $adapter = Storage::disk($this->disk);

        return $adapter->getClient();
    }

    private function bucket(): string
    {
        return (string) config("filesystems.disks.{$this->disk}.bucket");
    }
}
