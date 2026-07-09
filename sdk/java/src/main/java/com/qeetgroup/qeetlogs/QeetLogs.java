package com.qeetgroup.qeetlogs;

import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Duration;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Official Java SDK for Qeet Logs (PRD Module 01.1 / 29.1).
 *
 * Zero-config: one API key, one call to send a structured, searchable event.
 * Sends are best-effort by default — a network failure never throws into the
 * host application. Construct with {@code raiseOnError=true} to surface failures.
 *
 * <pre>
 *   QeetLogs logs = new QeetLogs(System.getenv("QEET_LOGS_API_KEY"), "http://localhost:8101");
 *   logs.log("payments", "charge failed", "error", Map.of("route", "/charge"));
 * </pre>
 */
public final class QeetLogs {
    private final String apiKey;
    private final String baseURL;
    private final HttpClient http;
    private final int retries;
    private final boolean raiseOnError;

    public QeetLogs(String apiKey, String baseURL) {
        this(apiKey, baseURL, 2, false);
    }

    public QeetLogs(String apiKey, String baseURL, int retries, boolean raiseOnError) {
        if (apiKey == null || apiKey.isEmpty()) {
            throw new IllegalArgumentException("qeetlogs: apiKey is required");
        }
        this.apiKey = apiKey;
        this.baseURL = baseURL.replaceAll("/+$", "");
        this.retries = retries;
        this.raiseOnError = raiseOnError;
        this.http = HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(10)).build();
    }

    /** Build and send one structured event; attrs become additional fields. */
    public void log(String service, String message, String level, Map<String, Object> attrs) {
        Map<String, Object> rec = new LinkedHashMap<>();
        rec.put("service", service);
        rec.put("message", message);
        rec.put("level", level == null || level.isEmpty() ? "info" : level);
        if (attrs != null) {
            rec.putAll(attrs);
        }
        ingest(rec);
    }

    /** Send a single record to /v1/ingest. */
    public void ingest(Map<String, Object> record) {
        post("/v1/ingest", toJson(record), "application/json");
    }

    /** Send many records to /v1/ingest/batch as newline-delimited JSON. */
    public void ingestBatch(List<Map<String, Object>> records) {
        if (records == null || records.isEmpty()) {
            return;
        }
        StringBuilder sb = new StringBuilder();
        for (Map<String, Object> r : records) {
            sb.append(toJson(r)).append('\n');
        }
        post("/v1/ingest/batch", sb.toString(), "application/x-ndjson");
    }

    private void post(String path, String body, String contentType) {
        RuntimeException last = null;
        for (int attempt = 0; attempt <= retries; attempt++) {
            try {
                HttpRequest req = HttpRequest.newBuilder(URI.create(baseURL + path))
                        .timeout(Duration.ofSeconds(10))
                        .header("X-Qeet-Api-Key", apiKey)
                        .header("Content-Type", contentType)
                        .POST(HttpRequest.BodyPublishers.ofString(body))
                        .build();
                HttpResponse<Void> res = http.send(req, HttpResponse.BodyHandlers.discarding());
                if (res.statusCode() < 400) {
                    return;
                }
                last = new RuntimeException("qeetlogs: HTTP " + res.statusCode());
                if (res.statusCode() >= 400 && res.statusCode() < 500) {
                    break; // client error: do not retry
                }
            } catch (Exception e) {
                last = new RuntimeException(e);
            }
            try {
                Thread.sleep(100L * (attempt + 1));
            } catch (InterruptedException ie) {
                Thread.currentThread().interrupt();
                break;
            }
        }
        if (raiseOnError && last != null) {
            throw last;
        }
    }

    // ── Minimal JSON encoding (no third-party dependency) ────────────────────
    static String toJson(Map<String, Object> m) {
        StringBuilder sb = new StringBuilder("{");
        boolean first = true;
        for (Map.Entry<String, Object> e : m.entrySet()) {
            if (!first) {
                sb.append(',');
            }
            first = false;
            sb.append(quote(e.getKey())).append(':').append(value(e.getValue()));
        }
        return sb.append('}').toString();
    }

    static String value(Object v) {
        if (v == null) {
            return "null";
        }
        if (v instanceof Number || v instanceof Boolean) {
            return v.toString();
        }
        return quote(v.toString());
    }

    static String quote(String s) {
        StringBuilder sb = new StringBuilder("\"");
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            switch (c) {
                case '"': sb.append("\\\""); break;
                case '\\': sb.append("\\\\"); break;
                case '\n': sb.append("\\n"); break;
                case '\r': sb.append("\\r"); break;
                case '\t': sb.append("\\t"); break;
                default:
                    if (c < 0x20) {
                        sb.append(String.format("\\u%04x", (int) c));
                    } else {
                        sb.append(c);
                    }
            }
        }
        return sb.append('"').toString();
    }
}
