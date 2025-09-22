# Kopia Maintenance Monitoring

This document describes how to monitor Kopia repository maintenance operations in VolSync.

## Overview

VolSync provides comprehensive monitoring and observability for Kopia maintenance operations through Prometheus metrics. These metrics help you:

- Track maintenance job execution and success rates
- Detect maintenance failures before they impact backups
- Monitor repository health and growth
- Optimize maintenance schedules based on actual performance

## Available Metrics

### Maintenance CronJob Metrics

| Metric Name | Type | Description | Labels |
|------------|------|-------------|--------|
| `volsync_kopia_maintenance_cronjob_created_total` | Counter | Total number of maintenance CronJobs created | obj_name, obj_namespace, role, operation, repository |
| `volsync_kopia_maintenance_cronjob_updated_total` | Counter | Total number of maintenance CronJobs updated | obj_name, obj_namespace, role, operation, repository |
| `volsync_kopia_maintenance_cronjob_deleted_total` | Counter | Total number of maintenance CronJobs deleted | obj_name, obj_namespace, role, operation, repository |
| `volsync_kopia_maintenance_cronjob_failures_total` | Counter | Total number of failed maintenance jobs | obj_name, obj_namespace, role, operation, repository, failure_reason |
| `volsync_kopia_maintenance_last_run_timestamp_seconds` | Gauge | Unix timestamp of the last successful maintenance run | obj_name, obj_namespace, role, operation, repository |
| `volsync_kopia_maintenance_duration_seconds` | Summary | Duration of maintenance operations in seconds | obj_name, obj_namespace, role, operation, repository |

### Existing Repository Health Metrics

| Metric Name | Type | Description | Labels |
|------------|------|-------------|--------|
| `volsync_kopia_maintenance_operations_total` | Counter | Total number of repository maintenance operations performed | obj_name, obj_namespace, role, operation, repository, maintenance_type |
| `volsync_kopia_repository_size_bytes` | Gauge | Total size of the Kopia repository in bytes | obj_name, obj_namespace, role, operation, repository |
| `volsync_kopia_repository_objects_total` | Gauge | Total number of objects in the Kopia repository | obj_name, obj_namespace, role, operation, repository |

## Status Reporting

The ReplicationSource status now includes enhanced maintenance information:

```yaml
status:
  kopia:
    lastMaintenance: "2025-01-22T02:00:00Z"
    nextScheduledMaintenance: "2025-01-23T02:00:00Z"
    maintenanceFailures: 0
    maintenanceCronJob: "kopia-maintenance-a1b2c3d4"
```

### Status Fields

- `lastMaintenance`: Timestamp of the last successful maintenance operation
- `nextScheduledMaintenance`: When the next maintenance is scheduled to run
- `maintenanceFailures`: Number of consecutive maintenance failures
- `maintenanceCronJob`: Name of the associated CronJob managing maintenance

## Setting Up Monitoring

### 1. Deploy ServiceMonitor

Ensure Prometheus scrapes VolSync metrics:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: volsync-metrics
  namespace: volsync-system
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: volsync-metrics
  endpoints:
  - port: metrics
    interval: 30s
    path: /metrics
```

### 2. Configure Alerts

Deploy the example alert rules:

```bash
kubectl apply -f examples/kopia/maintenance-alerts.yaml
```

### 3. Create Grafana Dashboard

Use these PromQL queries to create monitoring dashboards:

#### Maintenance Success Rate
```promql
sum(rate(volsync_kopia_maintenance_operations_total[1h])) by (repository)
/
(sum(rate(volsync_kopia_maintenance_operations_total[1h])) by (repository)
+ sum(rate(volsync_kopia_maintenance_cronjob_failures_total[1h])) by (repository))
```

#### Time Since Last Maintenance
```promql
time() - volsync_kopia_maintenance_last_run_timestamp_seconds
```

#### Maintenance Duration Trend
```promql
histogram_quantile(0.95,
  rate(volsync_kopia_maintenance_duration_seconds_bucket[1d])
)
```

#### Repository Growth Rate
```promql
rate(volsync_kopia_repository_size_bytes[1d])
```

## Example Prometheus Alerts

The provided alert rules include:

### Critical Alerts

- **KopiaMaintenanceCritical**: Maintenance hasn't run for over 7 days
- **KopiaMaintenanceNotRunning**: Maintenance hasn't run for over 3 days

### Warning Alerts

- **KopiaMaintenanceFailures**: Multiple maintenance job failures detected
- **KopiaMaintenanceSlow**: Maintenance operations taking longer than expected
- **KopiaMaintenanceCronJobMissing**: Backups running but no maintenance configured

### Info Alerts

- **KopiaMaintenanceCronJobChurn**: High rate of CronJob updates
- **KopiaRepositoryGrowth**: Repository growing rapidly
- **KopiaDeduplicationPoor**: Low deduplication ratio

## Troubleshooting

### Check Maintenance Job Status

```bash
# List all maintenance CronJobs
kubectl get cronjobs -A -l volsync.backube/kopia-maintenance=true

# Check recent maintenance jobs
kubectl get jobs -n <namespace> -l volsync.backube/kopia-maintenance=true --sort-by=.metadata.creationTimestamp

# View logs from the most recent maintenance job
kubectl logs -n <namespace> job/<job-name>
```

### Manually Trigger Maintenance

```bash
# Create a manual job from the CronJob
kubectl create job --from=cronjob/<cronjob-name> manual-maintenance-$(date +%s) -n <namespace>
```

### Check Metrics Endpoint

```bash
# Port-forward to the VolSync controller
kubectl port-forward -n volsync-system deployment/volsync 8080:8080

# Check metrics
curl http://localhost:8080/metrics | grep kopia_maintenance
```

## Best Practices

1. **Set Appropriate Schedules**: Configure maintenance to run during low-activity periods
2. **Monitor Regularly**: Set up dashboards and alerts before issues become critical
3. **Resource Allocation**: Ensure maintenance jobs have sufficient CPU and memory
4. **Retention Policies**: Configure appropriate retention to prevent excessive repository growth
5. **Alert Response**: Document procedures for responding to maintenance alerts

## Integration with Existing Monitoring

### Prometheus Operator

If using Prometheus Operator, the PrometheusRule resource will automatically be picked up:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: kopia-maintenance-alerts
  labels:
    prometheus: kube-prometheus
```

### Alertmanager Configuration

Route maintenance alerts appropriately:

```yaml
route:
  group_by: ['alertname', 'cluster', 'namespace']
  routes:
  - match:
      alert_type: maintenance
    receiver: ops-team
    group_interval: 6h
    repeat_interval: 12h
```

### Grafana Annotations

Add maintenance events as annotations:

```promql
changes(volsync_kopia_maintenance_last_run_timestamp_seconds[5m]) > 0
```

## Performance Considerations

- Metrics are designed to have minimal overhead
- Use appropriate scrape intervals (30s recommended)
- Consider metric cardinality when using many repositories
- Aggregate metrics at the repository level for overview dashboards

## Related Documentation

- [Kopia Backup Configuration](./backup-configuration.rst)
- [VolSync Metrics Reference](../../metrics.md)
- [Prometheus Alerting](https://prometheus.io/docs/alerting/latest/overview/)