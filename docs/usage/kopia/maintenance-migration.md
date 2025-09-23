# Migrating to KopiaMaintenance CRD

This document provides guidance for migrating from embedded maintenance configuration in ReplicationSource to the new KopiaMaintenance CRD.

## Overview

In previous versions of VolSync, Kopia maintenance was configured using embedded fields in the ReplicationSource specification:
- `maintenanceIntervalDays`
- `maintenanceCronJob`

Starting with VolSync v0.10.0, a new dedicated `KopiaMaintenance` CRD is available that provides:
- Better separation of concerns
- Centralized maintenance configuration
- Advanced repository matching
- Priority-based conflict resolution
- Multi-namespace repository management

## Migration Benefits

### Before (Embedded Configuration)
```yaml
apiVersion: volsync.backube/v1alpha1
kind: ReplicationSource
metadata:
  name: database-backup
  namespace: app1
spec:
  sourcePVC: database-data
  kopia:
    repository: "s3-backup-repo"
    maintenanceCronJob:
      enabled: true
      schedule: "0 2 * * *"
      successfulJobsHistoryLimit: 3
      failedJobsHistoryLimit: 1
      resources:
        requests:
          memory: "256Mi"
        limits:
          memory: "1Gi"
```

**Issues with embedded configuration:**
- Configuration duplicated across multiple ReplicationSources using the same repository
- Conflicts when multiple sources specify different schedules for the same repository
- No centralized control over repository maintenance
- Difficult to manage maintenance across multiple namespaces

### After (KopiaMaintenance CRD)
```yaml
apiVersion: volsync.backube/v1alpha1
kind: KopiaMaintenance
metadata:
  name: s3-backup-maintenance
spec:
  repositorySelector:
    repository: "s3-backup-repo"
    namespaceSelector:
      matchNames: ["app1", "app2"]
  schedule: "0 2 * * *"
  enabled: true
  priority: 10
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 1
  resources:
    requests:
      memory: "256Mi"
    limits:
      memory: "1Gi"
---
apiVersion: volsync.backube/v1alpha1
kind: ReplicationSource
metadata:
  name: database-backup
  namespace: app1
spec:
  sourcePVC: database-data
  kopia:
    repository: "s3-backup-repo"
    # No maintenance configuration needed - handled by KopiaMaintenance
```

**Benefits of KopiaMaintenance CRD:**
- Single configuration manages multiple ReplicationSources
- Clear conflict resolution using priority
- Advanced repository matching with wildcards
- Namespace-aware configuration
- Better observability with dedicated status fields

## Migration Steps

### Step 1: Identify Current Maintenance Configuration

List all ReplicationSources with embedded maintenance configuration:

```bash
kubectl get replicationsources -A -o jsonpath='{range .items[*]}{.metadata.namespace}{"\t"}{.metadata.name}{"\t"}{.spec.kopia.repository}{"\t"}{.spec.kopia.maintenanceCronJob.schedule}{"\n"}{end}' | grep -v 'null'
```

### Step 2: Create KopiaMaintenance Resources

For each unique repository configuration, create a KopiaMaintenance resource:

```yaml
apiVersion: volsync.backube/v1alpha1
kind: KopiaMaintenance
metadata:
  name: repository-name-maintenance
spec:
  repositorySelector:
    repository: "your-repository-secret-name"
    # Optional: restrict to specific namespaces
    namespaceSelector:
      matchNames: ["namespace1", "namespace2"]
  schedule: "0 2 * * *"  # Copy from existing maintenanceCronJob.schedule
  enabled: true
  priority: 0  # Higher priority wins conflicts
  # Copy other settings from existing maintenanceCronJob
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 1
  resources:
    requests:
      memory: "256Mi"
    limits:
      memory: "1Gi"
```

### Step 3: Apply KopiaMaintenance Resources

```bash
kubectl apply -f kopia-maintenance.yaml
```

### Step 4: Verify KopiaMaintenance Status

Check that the KopiaMaintenance resource is matching your ReplicationSources:

```bash
kubectl get kopiamaintenance -o wide
kubectl describe kopiamaintenance repository-name-maintenance
```

Look for the `MatchedSources` field in the status to confirm correct matching.

### Step 5: Remove Embedded Maintenance Configuration

Once KopiaMaintenance is working correctly, remove the embedded maintenance configuration from your ReplicationSources:

```bash
# Edit each ReplicationSource to remove maintenanceCronJob and maintenanceIntervalDays
kubectl edit replicationsource database-backup -n app1
```

Remove these fields:
```yaml
spec:
  kopia:
    # Remove these lines:
    maintenanceIntervalDays: 7
    maintenanceCronJob:
      enabled: true
      schedule: "0 2 * * *"
      # ... other maintenance settings
```

### Step 6: Verify Migration

Check that maintenance is still working:

```bash
# Check CronJob status
kubectl get cronjobs -A -l volsync.backube/kopia-maintenance=true

# Check ReplicationSource status for KopiaMaintenance reference
kubectl get replicationsource database-backup -n app1 -o jsonpath='{.status.kopia.kopiaMaintenance}'
```

## Advanced Configurations

### Wildcard Repository Matching

Match multiple repositories with patterns:

```yaml
apiVersion: volsync.backube/v1alpha1
kind: KopiaMaintenance
metadata:
  name: prod-backup-maintenance
spec:
  repositorySelector:
    repository: "prod-*-backup"  # Matches prod-db-backup, prod-app-backup, etc.
    namespaceSelector:
      matchLabels:
        environment: "production"
  schedule: "0 1 * * *"  # Different schedule for production
  priority: 100  # High priority for production
```

### Multiple Environment Management

Create separate maintenance configurations for different environments:

```yaml
# Production maintenance - high priority, nightly
apiVersion: volsyncv1alpha1
kind: KopiaMaintenance
metadata:
  name: production-maintenance
spec:
  repositorySelector:
    namespaceSelector:
      matchLabels:
        environment: "production"
  schedule: "0 1 * * *"
  priority: 100
  resources:
    requests:
      memory: "512Mi"
    limits:
      memory: "2Gi"
---
# Development maintenance - lower priority, weekly
apiVersion: volsync.backube/v1alpha1
kind: KopiaMaintenance
metadata:
  name: development-maintenance
spec:
  repositorySelector:
    namespaceSelector:
      matchLabels:
        environment: "development"
  schedule: "0 2 * * 0"  # Weekly on Sunday
  priority: 0
```

### Conflict Resolution

When multiple KopiaMaintenance resources match the same repository:
1. Higher priority wins
2. If priorities are equal, alphabetical name order determines precedence
3. Conflicting configurations are reported in the status

```yaml
# High priority maintenance for critical repositories
apiVersion: volsync.backube/v1alpha1
kind: KopiaMaintenance
metadata:
  name: critical-maintenance
spec:
  repositorySelector:
    repository: "critical-*"
  priority: 50
  schedule: "0 0 * * *"  # Midnight maintenance
---
# General maintenance with lower priority
apiVersion: volsync.backube/v1alpha1
kind: KopiaMaintenance
metadata:
  name: general-maintenance
spec:
  repositorySelector:
    repository: "*"
  priority: 0
  schedule: "0 2 * * *"  # 2 AM maintenance
```

## Troubleshooting

### Check Matching Logic

Use kubectl to debug repository matching:

```bash
# Check KopiaMaintenance status
kubectl describe kopiamaintenance

# Check ReplicationSource status
kubectl get replicationsource -A -o custom-columns=NAME:.metadata.name,NAMESPACE:.metadata.namespace,MAINTENANCE:.status.kopia.kopiaMaintenance
```

### Legacy Configuration Still Active

If legacy embedded configuration is still being used:

1. Check for deprecation warnings in the controller logs:
   ```bash
   kubectl logs -n volsync-system deployment/volsync-controller | grep DEPRECATION
   ```

2. Verify KopiaMaintenance is correctly matching:
   ```bash
   kubectl get kopiamaintenance -o jsonpath='{.items[*].status.matchedSources}'
   ```

3. Ensure no embedded maintenance configuration remains in ReplicationSources

### Maintenance Not Running

If maintenance CronJobs are not being created:

1. Check KopiaMaintenance status:
   ```bash
   kubectl describe kopiamaintenance
   ```

2. Look for controller errors:
   ```bash
   kubectl logs -n volsync-system deployment/volsync-controller | grep -i maintenance
   ```

3. Verify repository selector matches your ReplicationSources

## Best Practices

### 1. Repository Naming Conventions
Use consistent naming patterns to leverage wildcard matching:
```
prod-db-backup-s3
prod-app-backup-s3
dev-db-backup-s3
```

### 2. Namespace Organization
Organize namespaces with labels for easy maintenance targeting:
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: production-app
  labels:
    environment: production
    team: platform
```

### 3. Priority Assignment
Use clear priority ranges:
- 100+: Critical systems requiring special maintenance windows
- 50-99: Production systems
- 0-49: Development and testing systems
- Negative: Maintenance disabled or special cases

### 4. Resource Allocation
Size maintenance resources based on repository size and complexity:
```yaml
# For large repositories
resources:
  requests:
    memory: "1Gi"
    cpu: "500m"
  limits:
    memory: "4Gi"
    cpu: "2"

# For small repositories
resources:
  requests:
    memory: "256Mi"
    cpu: "100m"
  limits:
    memory: "1Gi"
    cpu: "500m"
```

## Compatibility

### Backward Compatibility
- Embedded maintenance configuration continues to work but is deprecated
- Migration can be done gradually per repository
- Both old and new configurations can coexist during migration
- Deprecation warnings are logged for embedded configuration

### Future Removal
- Embedded maintenance fields will be removed in VolSync v0.12.0
- Plan migration before upgrading to v0.12.0
- KopiaMaintenance CRD is the long-term solution

## Examples

See the `examples/kopia/maintenance/` directory for complete migration examples:
- `legacy-configuration.yaml` - Example of old embedded configuration
- `migrated-configuration.yaml` - Same setup using KopiaMaintenance CRD
- `multi-environment.yaml` - Advanced multi-environment setup
- `wildcard-matching.yaml` - Examples of wildcard repository matching