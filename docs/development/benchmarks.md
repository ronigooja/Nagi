# Proxy benchmark notes

This document records a comparative VLESS TCP+TLS latency sample for Nagi and Shadowrocket. It is a reference for the README feature summary, not a performance guarantee or a throughput ranking.

## Recorded comparison

The Nagi run took place on 2026-10-12. A macOS client connected over the LAN to a Linux VLESS TCP+TLS test service. The test used the server's default TCP ACK settings.

The Shadowrocket reference run took place on 2026-10-02 with the same general VLESS TCP+TLS test design, but in a different test session and network condition.

| Median request time | Nagi | Shadowrocket |
| --- | ---: | ---: |
| New connection | 5.42 ms | 53.36 ms |
| Reused request | 0.32 ms | 0.55 ms |

These are complete connection and request timings. They include transport, scheduling, TLS, HTTP service, and server processing; they are not client CPU time, download throughput, or a prediction for public relay nodes.

## QUICKACK diagnostic

The Shadowrocket session also ran a separate Linux server `TCP_QUICKACK` diagnostic. That setting reduced Shadowrocket's new-connection median to 6.50 ms. It was a test-server diagnostic variable, not a required Nagi setting. The Nagi run did not enable it.

The different dates, network conditions, and diagnostic settings mean the comparison is indicative only. The source records are maintained in the development checkout at `/root/proxy-client-bench/docs/RESULTS-nagi-2026-10-12.md` and `/root/proxy-client-bench/docs/RESULTS-shadowrocket-retest.md`.
