<?php

namespace App\Jobs;

use App\Models\Webhook;
use Illuminate\Bus\Queueable;
use Illuminate\Contracts\Queue\ShouldQueue;
use Illuminate\Foundation\Bus\Dispatchable;
use Illuminate\Queue\InteractsWithQueue;
use Illuminate\Queue\SerializesModels;
use Illuminate\Support\Facades\Http;

class SendWebhookJob implements ShouldQueue
{
    use Dispatchable, InteractsWithQueue, Queueable, SerializesModels;

    public int $tries = 3;

    public array $backoff = [5, 30, 120];

    public function __construct(
        public string $webhookId,
        public string $event,
        public array $payload,
    ) {}

    public function handle(): void
    {
        $webhook = Webhook::query()->find($this->webhookId);

        if ($webhook === null || ! $webhook->is_active) {
            return;
        }

        if (! in_array($this->event, $webhook->events, true)) {
            return;
        }

        Http::timeout(10)
            ->connectTimeout(5)
            ->retry(2, 1000)
            ->post($webhook->url, $this->payload);
    }
}
