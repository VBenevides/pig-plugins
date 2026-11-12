# Durable work reference

Read this reference only when a task produces durable output over many items. It supplements the primary instructions and does not authorize another mutation method.

1. Save useful progress incrementally. Track stage validity explicitly. A stored record does not prove that an earlier stage succeeded.
2. Resume only missing, pending, retryable-failed, or changed items. Skip unchanged successes. Make retries and resume idempotent. Avoid duplicate inserts, messages, uploads, and writes.
3. Isolate safe item-level failures, preserve the original error and item identity, and continue independent neighbors. Use an explicit bounded retry policy. Stop for missing prerequisites, corruption risks, security failures, or misleading results.
4. Bound concurrency, queues, retries, reads, memory, and open resources. Mark success only after required output is durable. Keep destructive cleanup separate from constructive processing.
5. Test interruption, resume, duplicate-effect prevention, one failed item beside successful neighbors, and interface compatibility. Report what was actually verified.

Use meaningful counters such as batch_processed, processed_total, batch_failures, failures_total, and remaining. Prefer clear boundaries over tiny helpers or unnecessary layers.
