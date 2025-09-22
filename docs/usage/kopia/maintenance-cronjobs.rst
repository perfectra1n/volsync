==============================
Kopia Maintenance CronJobs
==============================

.. sidebar:: Contents

   .. contents:: Maintenance CronJobs
      :local:

Overview
========

VolSync now provides a dedicated maintenance CronJob feature for Kopia repositories,
decoupling maintenance operations from backup jobs. This new approach provides
better resource management, flexible scheduling, and improved observability
compared to the legacy ``maintenanceIntervalDays`` approach.

Why Use Maintenance CronJobs?
=============================

**Decoupled Operations**
   Maintenance runs independently from backups, preventing backup delays when
   maintenance takes longer than expected.

**Resource Management**
   Dedicated resource limits for maintenance operations prevent them from
   competing with backup jobs for cluster resources.

**Flexible Scheduling**
   Standard cron syntax allows precise control over when maintenance runs,
   including different schedules for different environments.

**Better Observability**
   Separate CronJobs and Jobs provide clear visibility into maintenance
   status and history through Kubernetes tooling.

**Repository Deduplication**
   One CronJob per unique repository per namespace, regardless of how many
   ReplicationSources use that repository.

How It Works
============

When you configure a ReplicationSource with Kopia and enable maintenance CronJobs,
VolSync automatically:

1. **Creates a CronJob** in the same namespace as your ReplicationSource
2. **Uses repository deduplication** - multiple ReplicationSources sharing the
   same repository will share one maintenance CronJob
3. **Runs maintenance with username** ``maintenance@volsync`` to distinguish
   from backup operations
4. **Manages CronJob lifecycle** - updates configuration when ReplicationSources
   change, cleans up when no longer needed

Configuration
=============

Basic Configuration
-------------------

Enable maintenance CronJobs in your ReplicationSource:

.. code-block:: yaml

   apiVersion: volsync.backube/v1alpha1
   kind: ReplicationSource
   metadata:
     name: mydata-backup
   spec:
     sourcePVC: mydata
     trigger:
       schedule: "0 2 * * *"  # Backup at 2 AM
     kopia:
       repository: kopia-config
       # Enable maintenance CronJobs (default: enabled)
       maintenanceCronJob:
         enabled: true
         schedule: "0 3 * * *"  # Maintenance at 3 AM (after backups)

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

Repository Deduplication
=========================

VolSync automatically deduplicates maintenance CronJobs based on the repository
configuration. Multiple ReplicationSources sharing the same repository will
share a single maintenance CronJob.

**Repository Identification**

VolSync identifies unique repositories by creating a hash of:

- Repository URL/path
- Repository authentication (secrets, environment variables)
- Namespace (repositories are scoped to namespaces)

**Example Scenario**

.. code-block:: yaml

   # Both ReplicationSources share the same repository
   ---
   apiVersion: volsync.backube/v1alpha1
   kind: ReplicationSource
   metadata:
     name: app1-backup
   spec:
     kopia:
       repository: shared-kopia-config  # Same repository
       maintenanceCronJob:
         schedule: "0 2 * * *"

   ---
   apiVersion: volsync.backube/v1alpha1
   kind: ReplicationSource
   metadata:
     name: app2-backup
   spec:
     kopia:
       repository: shared-kopia-config  # Same repository
       maintenanceCronJob:
         schedule: "0 3 * * *"  # Different schedule - will be merged

**Result**: Only one maintenance CronJob is created. The schedule and
configuration are merged from all ReplicationSources using the repository.

Monitoring and Troubleshooting
===============================

Viewing Maintenance CronJobs
-----------------------------

List all maintenance CronJobs in a namespace:

.. code-block:: bash

   kubectl get cronjobs -l volsync.backube/maintenance-cronjob=true

View a specific maintenance CronJob:

.. code-block:: bash

   kubectl describe cronjob volsync-maintenance-<hash>

Checking Maintenance Job Status
-------------------------------

View recent maintenance jobs:

.. code-block:: bash

   kubectl get jobs -l volsync.backube/maintenance-cronjob=true

Check job logs:

.. code-block:: bash

   kubectl logs job/volsync-maintenance-<hash>-<timestamp>

Monitoring Metrics
------------------

VolSync exposes metrics for maintenance operations:

- ``maintenance_cronjob_created_total``: Number of maintenance CronJobs created
- ``maintenance_duration_seconds``: Duration of maintenance operations
- ``maintenance_success_total``: Number of successful maintenance runs
- ``maintenance_failure_total``: Number of failed maintenance runs

Common Issues
=============

Maintenance CronJob Not Created
-------------------------------

**Symptoms**: No maintenance CronJob appears after creating ReplicationSource.

**Possible Causes**:

1. Maintenance CronJobs are disabled:

   .. code-block:: yaml

      maintenanceCronJob:
        enabled: false

2. Invalid repository configuration prevents CronJob creation.

**Solutions**:

1. Ensure maintenance is enabled (default: true)
2. Check ReplicationSource status for repository validation errors:

   .. code-block:: bash

      kubectl describe replicationsource mydata-backup

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

Multiple CronJobs for Same Repository
-------------------------------------

**Symptoms**: Multiple maintenance CronJobs exist for what should be the same repository.

**Cause**: Repository configurations are not identical (different secrets, URLs, etc.).

**Solution**: Ensure all ReplicationSources using the same repository have
identical repository configurations.

Conflicting Schedules
---------------------

**Symptoms**: Maintenance runs at unexpected times when multiple ReplicationSources
share a repository.

**Explanation**: VolSync merges maintenance configurations from all ReplicationSources
sharing a repository. The resulting schedule may differ from individual configurations.

**Solution**: Coordinate maintenance schedules across ReplicationSources sharing
repositories, or use separate repositories for different maintenance schedules.

Best Practices
==============

Schedule Coordination
---------------------

1. **Stagger backup and maintenance**: Schedule maintenance after backups complete
2. **Avoid resource conflicts**: Stagger maintenance across different repositories
3. **Consider time zones**: Maintenance schedules use the controller's timezone

Resource Planning
-----------------

1. **Size appropriately**: Large repositories need more memory for maintenance
2. **Monitor actual usage**: Use metrics to right-size resource requests and limits
3. **Plan for peak usage**: Maintenance can be I/O intensive

Repository Design
-----------------

1. **Shared repositories**: Use shared repositories for better deduplication and
   simplified maintenance
2. **Separate when needed**: Use separate repositories when different maintenance
   schedules are required
3. **Namespace isolation**: Repositories are isolated per namespace

Operational Considerations
--------------------------

1. **Monitor regularly**: Set up alerts on maintenance job failures
2. **Plan maintenance windows**: Consider application impact during maintenance
3. **Test configuration changes**: Validate maintenance settings in non-production first

Next Steps
==========

- Learn about :doc:`troubleshooting` for comprehensive debugging guidance
- See :doc:`backup-configuration` for complete Kopia backup setup
- Review :doc:`../metrics/index` for monitoring and alerting setup
- Check :doc:`../resourcerequirements` for cluster resource planning