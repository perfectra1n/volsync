==============================
Kopia Maintenance CronJobs
==============================

.. sidebar:: Contents

   .. contents:: Maintenance CronJobs
      :local:

Overview
========

VolSync provides centralized maintenance CronJob management for Kopia repositories.
Starting with version 0.18.0, maintenance CronJobs are created in the VolSync
operator namespace (e.g., ``volsync-system``) rather than in source namespaces,
providing better resource efficiency and simplified management.

Key Features
============

**Centralized Management**
   All maintenance CronJobs are created in the VolSync operator namespace,
   providing a single location for monitoring and management.

**Automatic Deduplication**
   One CronJob per unique repository across all namespaces. Multiple
   ReplicationSources using the same repository automatically share a
   single maintenance CronJob.

**Decoupled Operations**
   Maintenance runs independently from backups, preventing backup delays when
   maintenance takes longer than expected.

**Resource Efficiency**
   Eliminates duplicate CronJobs for shared repositories, reducing cluster
   resource consumption.

**Flexible Scheduling**
   Standard cron syntax allows precise control over when maintenance runs,
   with automatic conflict resolution for shared repositories.

**Better Observability**
   Centralized CronJobs provide clear visibility into maintenance
   status and history through standard Kubernetes tooling.

Architecture Changes (v0.18.0+)
================================

Centralized Management
----------------------

Maintenance CronJobs are now created in the VolSync operator namespace
(typically ``volsync-system``) instead of source namespaces. This provides:

- **Single management point**: All maintenance operations in one namespace
- **Simplified monitoring**: Check all maintenance status in one location
- **Resource efficiency**: No duplicate CronJobs across namespaces
- **Automatic migration**: Existing CronJobs are migrated automatically

How It Works
------------

When you configure a ReplicationSource with Kopia and enable maintenance CronJobs,
VolSync automatically:

1. **Creates a CronJob** in the operator namespace (``volsync-system``)
2. **Copies repository secrets** from source namespace to operator namespace
3. **Deduplicates across namespaces** - repositories used in multiple namespaces
   share one maintenance CronJob
4. **Manages secret lifecycle** - updates copied secrets when source changes,
   cleans up when no longer needed
5. **Runs maintenance with username** ``maintenance@volsync`` to distinguish
   from backup operations, with automatic ownership enforcement
6. **Handles schedule conflicts** - uses first-wins strategy when the same
   repository has different schedules

Configuration
=============

Basic Configuration
-------------------

The user-facing configuration remains unchanged. Enable maintenance CronJobs
in your ReplicationSource:

.. code-block:: yaml

   apiVersion: volsync.backube/v1alpha1
   kind: ReplicationSource
   metadata:
     name: mydata-backup
     namespace: myapp  # Your application namespace
   spec:
     sourcePVC: mydata
     trigger:
       schedule: "0 2 * * *"  # Backup at 2 AM
     kopia:
       repository: kopia-config  # Secret in myapp namespace
       # Enable maintenance CronJobs (default: enabled)
       maintenanceCronJob:
         enabled: true
         schedule: "0 3 * * *"  # Maintenance at 3 AM

.. note::
   The CronJob will be created in ``volsync-system``, not in your namespace.
   Your repository secret remains in your namespace and is automatically copied.

Complete Configuration
----------------------

All available maintenance CronJob options:

.. code-block:: yaml

   apiVersion: volsync.backube/v1alpha1
   kind: ReplicationSource
   metadata:
     name: mydata-backup
   spec:
     sourcePVC: mydata
     trigger:
       schedule: "0 2 * * *"
     kopia:
       repository: kopia-config
       maintenanceCronJob:
         # Enable or disable maintenance CronJobs
         enabled: true  # default: true

         # Cron schedule for maintenance (controller timezone)
         schedule: "0 2 * * *"  # default: "0 2 * * *" (2 AM daily)

         # Job history limits
         successfulJobsHistoryLimit: 3  # default: 3
         failedJobsHistoryLimit: 1      # default: 1

         # Temporarily suspend maintenance
         suspend: false  # default: false

         # Resource requirements for maintenance
         resources:
           requests:
             cpu: "100m"
             memory: "256Mi"
           limits:
             cpu: "500m"
             memory: "512Mi"

Default Resource Limits
-----------------------

When not specified, maintenance CronJobs use these resource limits:

.. code-block:: yaml

   resources:
     requests:
       cpu: "100m"
       memory: "256Mi"
     limits:
       cpu: "500m"
       memory: "512Mi"

These defaults are optimized for typical maintenance operations while preventing
resource exhaustion.

Migration Guide
===============

From maintenanceIntervalDays to MaintenanceCronJob
---------------------------------------------------

The legacy ``maintenanceIntervalDays`` field is deprecated in favor of the new
``maintenanceCronJob`` configuration. Here's how to migrate:

**Before (Legacy)**:

.. code-block:: yaml

   apiVersion: volsync.backube/v1alpha1
   kind: ReplicationSource
   metadata:
     name: mydata-backup
   spec:
     sourcePVC: mydata
     kopia:
       repository: kopia-config
       maintenanceIntervalDays: 7  # deprecated

**After (Recommended)**:

.. code-block:: yaml

   apiVersion: volsync.backube/v1alpha1
   kind: ReplicationSource
   metadata:
     name: mydata-backup
   spec:
     sourcePVC: mydata
     kopia:
       repository: kopia-config
       maintenanceCronJob:
         enabled: true
         schedule: "0 2 * * 0"  # Weekly on Sunday at 2 AM

Migration Compatibility
-----------------------

During migration, both approaches can coexist:

- If only ``maintenanceIntervalDays`` is specified, it continues to work (deprecated)
- If only ``maintenanceCronJob`` is specified, it takes precedence
- If both are specified, ``maintenanceCronJob`` takes precedence and ``maintenanceIntervalDays`` is ignored

.. warning::
   ``maintenanceIntervalDays`` is deprecated and will be removed in a future version.
   Migrate to ``maintenanceCronJob`` for new features and continued support.

Maintenance Ownership
=====================

Kopia requires a single user to "own" maintenance operations for each repository.
VolSync automatically manages this ownership:

- **Maintenance Identity**: All maintenance operations use ``maintenance@volsync`` as the client identity
- **Ownership Enforcement**: The maintenance CronJob automatically claims ownership before running maintenance
- **Conflict Handling**: If another user owns maintenance, the job will retry based on CronJob configuration
- **Automatic Recovery**: Ownership is reclaimed if the previous owner is no longer active

This ensures reliable maintenance operations even in multi-tenant environments where
multiple namespaces share the same Kopia repository.

Common Configurations
=====================

Daily Maintenance
-----------------

Run maintenance daily at 2 AM:

.. code-block:: yaml

   maintenanceCronJob:
     schedule: "0 2 * * *"

Weekly Maintenance
------------------

Run maintenance weekly on Sunday at 3 AM:

.. code-block:: yaml

   maintenanceCronJob:
     schedule: "0 3 * * 0"

Staggered Maintenance
---------------------

For multiple repositories, stagger maintenance to avoid resource conflicts:

.. code-block:: yaml

   # Repository A - maintenance at 2 AM
   maintenanceCronJob:
     schedule: "0 2 * * *"

   # Repository B - maintenance at 3 AM
   maintenanceCronJob:
     schedule: "0 3 * * *"

High-Resource Maintenance
-------------------------

For large repositories requiring more resources:

.. code-block:: yaml

   maintenanceCronJob:
     schedule: "0 1 * * 0"  # Weekly during low-usage hours
     resources:
       requests:
         cpu: "500m"
         memory: "1Gi"
       limits:
         cpu: "2"
         memory: "4Gi"

Disabled Maintenance
--------------------

Temporarily disable maintenance (not recommended for production):

.. code-block:: yaml

   maintenanceCronJob:
     enabled: false

Deduplication and Secret Management
====================================

Cross-Namespace Deduplication
------------------------------

VolSync deduplicates maintenance CronJobs across all namespaces. The system
identifies unique repositories based on:

- Repository secret name
- CustomCA configuration (if present)
- Secret contents (repository identity)

.. important::
   Namespace is NOT part of the repository hash. The same repository used
   in different namespaces will share one maintenance CronJob.

**Example: Multiple Namespaces, Same Repository**

.. code-block:: yaml

   # In namespace-a
   ---
   apiVersion: volsync.backube/v1alpha1
   kind: ReplicationSource
   metadata:
     name: app-a-backup
     namespace: namespace-a
   spec:
     kopia:
       repository: shared-kopia-secret
       maintenanceCronJob:
         schedule: "0 2 * * *"

   ---
   # In namespace-b
   apiVersion: volsync.backube/v1alpha1
   kind: ReplicationSource
   metadata:
     name: app-b-backup
     namespace: namespace-b
   spec:
     kopia:
       repository: shared-kopia-secret  # Same repository name
       maintenanceCronJob:
         schedule: "0 3 * * *"  # Different schedule

**Result**: Only ONE maintenance CronJob is created in ``volsync-system``.
The first schedule encountered ("0 2 * * *") is used due to first-wins strategy.

Secret Management
-----------------

VolSync automatically manages repository secrets:

1. **Automatic Copying**: Secrets are copied from source namespaces to the
   operator namespace with naming pattern: ``maintenance-{namespace}-{secretName}``

2. **Automatic Updates**: When source secrets change, copied secrets are
   automatically updated

3. **Automatic Cleanup**: Orphaned secrets are removed when no longer referenced

4. **Security Boundaries**: Original secrets remain in source namespaces,
   maintaining namespace isolation

Schedule Conflict Resolution
-----------------------------

When multiple ReplicationSources use the same repository with different schedules:

1. **First-wins strategy**: The first schedule encountered is used
2. **Conflict tracking**: Conflicts are recorded in CronJob annotations
3. **Visibility**: Use annotations to identify schedule conflicts

.. code-block:: bash

   # Check for schedule conflicts
   kubectl get cronjobs -n volsync-system \
     -l volsync.backube/kopia-maintenance=true \
     -o jsonpath='{range .items[*]}{.metadata.name}: {.metadata.annotations.volsync\.backube/schedule-conflict}{"\n"}{end}'

Monitoring and Observability
=============================

Viewing Maintenance CronJobs
-----------------------------

All maintenance CronJobs are now in the operator namespace:

.. code-block:: bash

   # List all maintenance CronJobs
   kubectl get cronjobs -n volsync-system \
     -l volsync.backube/kopia-maintenance=true

   # View details with source namespaces
   kubectl get cronjobs -n volsync-system \
     -l volsync.backube/kopia-maintenance=true \
     -o custom-columns=NAME:.metadata.name,SCHEDULE:.spec.schedule,NAMESPACES:.metadata.labels.volsync\.backube/source-namespaces

   # View repository hash for a specific CronJob
   kubectl get cronjob kopia-maintenance-<hash> -n volsync-system \
     -o jsonpath='{.metadata.labels.volsync\.backube/repository-hash}'

Checking Maintenance Job Status
-------------------------------

View recent maintenance jobs:

.. code-block:: bash

   # All maintenance jobs
   kubectl get jobs -n volsync-system \
     -l volsync.backube/kopia-maintenance=true

   # Check job logs
   kubectl logs -n volsync-system job/kopia-maintenance-<hash>-<timestamp>

Verifying Deduplication
------------------------

Count CronJobs per repository:

.. code-block:: bash

   # Should show 1 CronJob per unique repository hash
   kubectl get cronjobs -n volsync-system \
     -l volsync.backube/kopia-maintenance=true \
     -o jsonpath='{range .items[*]}{.metadata.labels.volsync\.backube/repository-hash}{"\n"}{end}' \
     | sort | uniq -c

Viewing Copied Secrets
-----------------------

List maintenance secrets in operator namespace:

.. code-block:: bash

   # View all copied maintenance secrets
   kubectl get secrets -n volsync-system \
     | grep "^maintenance-"

   # Check which namespace a secret came from
   kubectl get secret maintenance-myapp-kopia-config -n volsync-system \
     -o jsonpath='{.metadata.labels.volsync\.backube/source-namespace}'

Monitoring Metrics
------------------

VolSync exposes metrics for maintenance operations:

- ``maintenance_cronjob_created_total``: Number of maintenance CronJobs created
- ``maintenance_duration_seconds``: Duration of maintenance operations
- ``maintenance_success_total``: Number of successful maintenance runs
- ``maintenance_failure_total``: Number of failed maintenance runs

Troubleshooting
===============

Maintenance CronJob Not in volsync-system
------------------------------------------

**Symptoms**: No maintenance CronJob appears in ``volsync-system`` after creating ReplicationSource.

**Possible Causes**:

1. Maintenance CronJobs are disabled:

   .. code-block:: yaml

      maintenanceCronJob:
        enabled: false

2. Invalid repository configuration prevents CronJob creation
3. Operator lacks permissions to read secrets from source namespace
4. Using VolSync version older than 0.18.0

**Solutions**:

1. Ensure maintenance is enabled (default: true)
2. Check ReplicationSource status for errors:

   .. code-block:: bash

      kubectl describe replicationsource -n <namespace> <name>

3. Verify VolSync version:

   .. code-block:: bash

      kubectl get deployment -n volsync-system volsync \
        -o jsonpath='{.spec.template.spec.containers[0].image}'

4. Check operator logs for permission errors:

   .. code-block:: bash

      kubectl logs -n volsync-system deployment/volsync

Maintenance Jobs Failing
-------------------------

**Symptoms**: Maintenance jobs show failed status.

**Troubleshooting Steps**:

1. Check job logs:

   .. code-block:: bash

      kubectl logs job/volsync-maintenance-<hash>-<timestamp>

2. Common issues:
   - Repository authentication failures
   - Insufficient resources
   - Network connectivity issues

3. Verify repository secret is accessible:

   .. code-block:: bash

      kubectl get secret kopia-config

Resource Constraints
---------------------

**Symptoms**: Maintenance jobs are killed or fail due to resource limits.

**Solutions**:

1. Increase resource limits:

   .. code-block:: yaml

      maintenanceCronJob:
        resources:
          requests:
            memory: "512Mi"
          limits:
            memory: "2Gi"

2. Schedule maintenance during low-usage periods:

   .. code-block:: yaml

      maintenanceCronJob:
        schedule: "0 1 * * 0"  # Weekly at 1 AM Sunday

Duplicate CronJobs After Migration
-----------------------------------

**Symptoms**: Old CronJobs exist in source namespaces alongside new ones in ``volsync-system``.

**Cause**: Migration from pre-0.18.0 version may not have cleaned up all old resources.

**Solution**: Manually clean up old CronJobs and Jobs:

.. code-block:: bash

   # List old maintenance CronJobs in source namespaces
   kubectl get cronjobs --all-namespaces \
     -l volsync.backube/maintenance-cronjob=true \
     | grep -v volsync-system

   # Delete old CronJobs (replace namespace and name)
   kubectl delete cronjob -n <old-namespace> <old-cronjob-name>

Schedule Conflicts Not Resolved
--------------------------------

**Symptoms**: Maintenance runs at unexpected times when multiple ReplicationSources
share a repository with different schedules.

**Explanation**: VolSync uses a first-wins strategy. The first ReplicationSource
processed determines the schedule.

**Solution**:

1. Check which schedule is active:

   .. code-block:: bash

      kubectl get cronjob -n volsync-system kopia-maintenance-<hash> \
        -o jsonpath='{.spec.schedule}'

2. View schedule conflicts:

   .. code-block:: bash

      kubectl get cronjob -n volsync-system kopia-maintenance-<hash> \
        -o jsonpath='{.metadata.annotations.volsync\.backube/schedule-conflict}'

3. Coordinate schedules across teams using the same repository

Secret Copy Failures
--------------------

**Symptoms**: Maintenance jobs fail with authentication errors.

**Possible Causes**:

1. Source secret doesn't exist or was deleted
2. Operator lacks permissions to read secrets
3. Secret copy is out of sync

**Solutions**:

1. Verify source secret exists:

   .. code-block:: bash

      kubectl get secret -n <source-namespace> <secret-name>

2. Check copied secret in operator namespace:

   .. code-block:: bash

      kubectl get secret -n volsync-system maintenance-<namespace>-<secret-name>

3. Force secret resync by updating ReplicationSource:

   .. code-block:: bash

      kubectl annotate replicationsource -n <namespace> <name> \
        volsync.backube/resync="$(date)" --overwrite

Migration from Pre-0.18.0
=========================

Automatic Migration
-------------------

When upgrading to VolSync 0.18.0 or later:

1. **Automatic detection**: VolSync identifies existing maintenance CronJobs
2. **Centralized creation**: New CronJobs are created in ``volsync-system``
3. **Old cleanup**: Previous CronJobs and Jobs are removed from source namespaces
4. **Zero downtime**: Migration happens seamlessly without interrupting maintenance

**No manual intervention required** - the migration is fully automatic.

Verifying Migration
-------------------

After upgrade, verify successful migration:

.. code-block:: bash

   # Check new CronJobs in operator namespace
   kubectl get cronjobs -n volsync-system \
     -l volsync.backube/kopia-maintenance=true

   # Verify no old CronJobs remain
   kubectl get cronjobs --all-namespaces \
     -l volsync.backube/maintenance-cronjob=true \
     | grep -v volsync-system

   # Should return empty - all CronJobs now in volsync-system

Best Practices
==============

Schedule Coordination
---------------------

1. **Coordinate across teams**: When sharing repositories across namespaces,
   agree on maintenance schedules
2. **Document shared repositories**: Maintain a registry of shared repositories
   and their agreed schedules
3. **Use consistent schedules**: For shared repositories, use the same schedule
   in all ReplicationSources to avoid conflicts

Resource Planning
-----------------

1. **Centralized monitoring**: Monitor all maintenance from ``volsync-system``
2. **Resource quotas**: Ensure ``volsync-system`` namespace has adequate
   resource quotas for all maintenance jobs
3. **Size for peak**: Account for all maintenance jobs that might run
   concurrently

Security Considerations
------------------------

1. **Secret isolation**: Original secrets remain in source namespaces
2. **Least privilege**: Operator only needs read access to repository secrets
3. **Audit logging**: Monitor secret copy operations in operator logs

Operational Excellence
----------------------

1. **Single monitoring point**: Set up alerts on ``volsync-system`` namespace
2. **Simplified debugging**: All maintenance logs in one namespace
3. **Batch operations**: Manage all maintenance CronJobs with single commands
4. **Version consistency**: Ensure all namespaces use compatible VolSync versions

Benefits Summary
================

The centralized maintenance architecture provides:

1. **Resource Efficiency**: Eliminates duplicate CronJobs for shared repositories
2. **Centralized Management**: All maintenance operations in one namespace
3. **Simplified Monitoring**: Single location to check maintenance status
4. **Automatic Deduplication**: No manual coordination needed between teams
5. **Namespace Isolation**: Secrets are copied, maintaining security boundaries
6. **Zero-downtime Migration**: Automatic upgrade from previous versions

Requirements
============

- **VolSync version**: 0.18.0 or later
- **Helm chart version**: 0.18.0 or later (if using Helm)
- **Permissions**: Operator requires cluster-wide secret read permissions
  (included in standard deployment)
- **Namespace**: ``volsync-system`` or configured operator namespace must exist

Next Steps
==========

- Learn about :doc:`troubleshooting` for comprehensive debugging guidance
- See :doc:`backup-configuration` for complete Kopia backup setup
- Review :doc:`../metrics/index` for monitoring and alerting setup
- Check :doc:`../resourcerequirements` for cluster resource planning
- Explore :doc:`maintenance-monitoring` for detailed monitoring strategies