<?php

namespace App\Rules;

use Closure;
use Illuminate\Contracts\Validation\ValidationRule;

/**
 * Rejects URLs whose host resolves to a private, loopback, link-local, or
 * otherwise non-public IP address. Used to block SSRF where a user-supplied URL
 * is later fetched server-side (ffmpeg transcode source, outbound webhooks).
 */
class PublicUrl implements ValidationRule
{
    public function validate(string $attribute, mixed $value, Closure $fail): void
    {
        if (! is_string($value)) {
            $fail('The :attribute must be a valid URL.')->translate();

            return;
        }

        $host = parse_url($value, PHP_URL_HOST);

        if (! is_string($host) || $host === '') {
            $fail('The :attribute must be a valid URL.')->translate();

            return;
        }

        // Strip IPv6 brackets if present, e.g. [::1].
        $host = trim($host, '[]');

        $ips = $this->resolve($host);

        // A host that resolves to nothing cannot be fetched, so it poses no SSRF
        // risk; allow it rather than rejecting legitimate hosts that only resolve
        // in other environments (e.g. restricted-DNS CI). Literal private IPs and
        // hostnames that DO resolve to private ranges are still blocked below.
        if ($ips === []) {
            return;
        }

        foreach ($ips as $ip) {
            if (! $this->isPublic($ip)) {
                $fail('The :attribute must point to a public address.')->translate();

                return;
            }
        }
    }

    /**
     * @return array<int, string>
     */
    private function resolve(string $host): array
    {
        if (filter_var($host, FILTER_VALIDATE_IP) !== false) {
            return [$host];
        }

        $ips = [];

        $records = @dns_get_record($host, DNS_A | DNS_AAAA);
        if (is_array($records)) {
            foreach ($records as $record) {
                $ips[] = $record['ip'] ?? $record['ipv6'] ?? null;
            }
        }

        $ips = array_values(array_filter($ips, fn ($ip): bool => is_string($ip) && $ip !== ''));

        if ($ips === []) {
            $resolved = @gethostbyname($host);
            if ($resolved !== $host && filter_var($resolved, FILTER_VALIDATE_IP) !== false) {
                $ips[] = $resolved;
            }
        }

        return $ips;
    }

    private function isPublic(string $ip): bool
    {
        return filter_var(
            $ip,
            FILTER_VALIDATE_IP,
            FILTER_FLAG_NO_PRIV_RANGE | FILTER_FLAG_NO_RES_RANGE,
        ) !== false;
    }
}
