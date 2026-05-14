<?php

namespace App\Console\Commands;

use App\Jobs\SendWebhookJob;
use App\Models\Webhook;
use Illuminate\Console\Command;
use Illuminate\Support\Facades\Redis;

class ConsumeWebhooksCommand extends Command
{
    protected $signature = 'webhooks:consume {--timeout=30}';

    protected $description = 'Consume webhook events from Redis and dispatch delivery jobs';

    public function handle(): int
    {
        $queueKey = config('services.transcode.webhook_queue_key');
        $timeout = (int) $this->option('timeout');

        $this->info("Consuming webhook events from [{$queueKey}]");

        while (true) {
            $result = Redis::brpop($queueKey, $timeout);

            if ($result === null) {
                continue;
            }

            $raw = is_array($result) ? $result[1] : $result;

            $event = json_decode($raw, true);

            if ($event === null || ! isset($event['organization_id'], $event['event'])) {
                $this->warn('Invalid webhook event payload: '.$raw);

                continue;
            }

            $this->dispatchToWebhooks($event);
        }

        return self::SUCCESS;
    }

    private function dispatchToWebhooks(array $event): void
    {
        $organizationId = $event['organization_id'];
        $eventType = $event['event'];

        $webhooks = Webhook::query()
            ->where('organization_id', $organizationId)
            ->where('is_active', true)
            ->get();

        if ($webhooks->isEmpty()) {
            return;
        }

        $payload = [
            'event' => $eventType,
            'title' => $event['title'] ?? '',
            'file_name' => $event['file_name'] ?? '',
            'status' => $event['status'] ?? '',
            'tags' => $event['tags'] ?? [],
        ];

        foreach ($webhooks as $webhook) {
            SendWebhookJob::dispatch($webhook->id, $eventType, $payload);
        }

        $this->info("Dispatched {$eventType} to {$webhooks->count()} webhook(s) for org [{$organizationId}]");
    }
}
