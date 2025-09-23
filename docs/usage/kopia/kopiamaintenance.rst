==============================
KopiaMaintenance CRD Reference
==============================

.. sidebar:: Contents

   .. contents:: KopiaMaintenance
      :local:

Overview
========

The KopiaMaintenance Custom Resource Definition (CRD) provides streamlined management of Kopia repository maintenance operations in VolSync. This namespace-scoped resource offers a simple, direct approach to configuring maintenance schedules for your Kopia repositories.

What is KopiaMaintenance?
-------------------------

KopiaMaintenance is a Kubernetes custom resource that manages automated maintenance operations for Kopia repositories. It creates and manages CronJobs that perform essential repository maintenance tasks including:

- Garbage collection of unused data blocks
- Repository compaction and optimization
- Index maintenance for improved performance
- Verification of repository integrity

Key Features
------------

- **Namespace-scoped**: Each KopiaMaintenance resource manages repositories within its namespace
- **Direct repository configuration**: Explicit 1:1 mapping between maintenance resources and repositories
- **Simple API**: Focused design without complex selectors or priority systems
- **Resource management**: Configure CPU and memory limits for maintenance operations
- **Flexible scheduling**: Support for standard cron expressions and aliases

When to Use KopiaMaintenance
----------------------------

**Use KopiaMaintenance when you need:**

- Automated maintenance for Kopia repositories
- Namespace-isolated maintenance management
- Clear, explicit maintenance configuration
- Control over maintenance resource consumption
- Simple deployment without cross-namespace complexity

**Continue using embedded maintenanceCronJob in ReplicationSource when:**

- You have existing configurations that work well
- You prefer configuration alongside your backup definitions
- You need minimal setup for single repositories

API Specification
=================

Basic Structure
---------------

.. code-block:: yaml

   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: <maintenance-name>
     namespace: <target-namespace>
   spec:
     repository:
       repository: <repository-secret-name>
       customCA:  # Optional
         configMapName: <ca-configmap-name>
         key: <ca-cert-key>
     schedule: "0 2 * * *"
     enabled: true
     suspend: false
     successfulJobsHistoryLimit: 3
     failedJobsHistoryLimit: 1
     resources:
       requests:
         memory: "256Mi"
         cpu: "100m"
       limits:
         memory: "1Gi"
         cpu: "500m"

Field Reference
---------------

Required Fields
^^^^^^^^^^^^^^^

**repository** (*KopiaRepositorySpec*, required)
   Defines the repository configuration for maintenance.
   The repository secret must exist in the same namespace as the KopiaMaintenance resource.

**repository.repository** (*string*, required)
   Name of the secret containing repository configuration.
   Secret must contain Kopia repository connection details (URL, credentials, etc.)

Optional Fields
^^^^^^^^^^^^^^^

**repository.customCA** (*ReplicationSourceKopiaCA*, optional)
   Custom CA configuration for repository access.

   - **configMapName**: Name of ConfigMap containing CA certificate
   - **key**: Key within ConfigMap containing the certificate (default: "ca.crt")
   - **secretName**: Alternative to ConfigMap, name of Secret containing CA certificate

**schedule** (*string*, optional)
   Cron schedule for maintenance execution.

   - Default: ``"0 2 * * *"`` (daily at 2 AM)
   - Supports standard cron expressions and aliases (``@daily``, ``@weekly``, ``@monthly``)

**enabled** (*boolean*, optional)
   Determines if maintenance should be performed.

   - Default: ``true``
   - When ``false``, no maintenance jobs will be created

**suspend** (*boolean*, optional)
   Temporarily stop maintenance without deleting configuration.

   - Default: ``false``
   - When ``true``, prevents new Jobs from being created while allowing existing Jobs to complete

**successfulJobsHistoryLimit** (*integer*, optional)
   Number of successful maintenance Jobs to retain.

   - Default: ``3``
   - Minimum: ``0``

**failedJobsHistoryLimit** (*integer*, optional)
   Number of failed maintenance Jobs to retain.

   - Default: ``1``
   - Minimum: ``0``

**resources** (*ResourceRequirements*, optional)
   Compute resources for maintenance containers.

   - Default requests: 256Mi memory
   - Default limits: 1Gi memory
   - Configure based on repository size and performance requirements

**serviceAccountName** (*string*, optional)
   Custom ServiceAccount for maintenance jobs.
   If not specified, uses default maintenance ServiceAccount.

**moverPodLabels** (*map[string]string*, optional)
   Additional labels for maintenance pods.
   Applied alongside VolSync-managed labels.

**affinity** (*Affinity*, optional)
   Pod affinity rules for maintenance jobs.
   Supports nodeAffinity, podAffinity, and podAntiAffinity.

Status Fields
^^^^^^^^^^^^^

The KopiaMaintenance controller updates these status fields:

**activeCronJob** (*string*)
   Name of the currently active CronJob managing maintenance.
   Empty if no CronJob is active.

**lastReconcileTime** (*Time*)
   Timestamp of the last successful reconciliation.

**lastMaintenanceTime** (*Time*)
   Timestamp of the last successful maintenance operation.

**nextScheduledMaintenance** (*Time*)
   Next scheduled maintenance execution time.

**maintenanceFailures** (*integer*)
   Count of consecutive maintenance failures.

**conditions** (*[]Condition*)
   Current state observations of the maintenance configuration.
   Common conditions: Ready, Reconciling, Error.

Configuration Examples
======================

Basic Daily Maintenance
-----------------------

.. code-block:: yaml

   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: daily-maintenance
     namespace: my-app
   spec:
     repository:
       repository: kopia-repository-secret
     schedule: "0 3 * * *"  # 3 AM daily
     enabled: true

Weekly Maintenance with Resource Limits
----------------------------------------

.. code-block:: yaml

   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: weekly-maintenance
     namespace: production
   spec:
     repository:
       repository: prod-backup-config
     schedule: "0 2 * * 0"  # 2 AM on Sundays
     resources:
       requests:
         memory: "512Mi"
         cpu: "200m"
       limits:
         memory: "2Gi"
         cpu: "1"
     successfulJobsHistoryLimit: 5
     failedJobsHistoryLimit: 2

Maintenance with Custom CA
--------------------------

.. code-block:: yaml

   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: secure-maintenance
     namespace: secure-backups
   spec:
     repository:
       repository: private-s3-config
       customCA:
         configMapName: company-ca-bundle
         key: ca-bundle.crt
     schedule: "0 1 * * 1,4"  # 1 AM on Mondays and Thursdays
     moverPodLabels:
       environment: production
       team: platform

High-Performance Maintenance
-----------------------------

.. code-block:: yaml

   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: large-repo-maintenance
     namespace: data-warehouse
   spec:
     repository:
       repository: warehouse-backup-config
     schedule: "0 0 * * 6"  # Midnight on Saturdays
     resources:
       requests:
         memory: "2Gi"
         cpu: "1"
       limits:
         memory: "8Gi"
         cpu: "4"
     affinity:
       nodeAffinity:
         requiredDuringSchedulingIgnoredDuringExecution:
           nodeSelectorTerms:
           - matchExpressions:
             - key: node-type
               operator: In
               values: ["high-memory"]

Temporarily Suspended Maintenance
----------------------------------

.. code-block:: yaml

   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: suspended-maintenance
     namespace: testing
   spec:
     repository:
       repository: test-backup-config
     schedule: "0 4 * * *"
     enabled: true
     suspend: true  # Temporarily suspended
     successfulJobsHistoryLimit: 10  # Keep more history during suspension

Best Practices
==============

Repository Secret Management
----------------------------

1. **Keep secrets in the same namespace**: The repository secret must exist in the same namespace as the KopiaMaintenance resource
2. **Use descriptive secret names**: Choose names that clearly identify the repository purpose (e.g., ``prod-s3-backup-config``, ``dev-gcs-repo``)
3. **Secure sensitive data**: Ensure repository secrets are properly protected with RBAC

Scheduling Considerations
-------------------------

1. **Avoid peak hours**: Schedule maintenance during low-activity periods
2. **Stagger multiple maintenances**: If managing multiple repositories, use different schedules to avoid resource contention
3. **Consider repository size**: Large repositories may need weekly rather than daily maintenance
4. **Account for time zones**: Schedules are interpreted in the controller's timezone

Resource Allocation
-------------------

1. **Start conservative**: Begin with default resources and adjust based on observed usage
2. **Monitor maintenance jobs**: Check job completion times and resource consumption
3. **Scale for repository size**: Larger repositories require more memory and CPU
4. **Use node affinity**: Direct maintenance to appropriate nodes for large-scale operations

Naming Conventions
------------------

1. **Use descriptive names**: ``prod-daily-maintenance``, ``staging-weekly-cleanup``
2. **Include frequency**: Indicate maintenance schedule in the name when relevant
3. **Match repository purpose**: Align maintenance names with repository naming

Migration Guide
===============

From Embedded maintenanceCronJob
---------------------------------

If you're currently using embedded maintenance configuration in ReplicationSource:

**Before (Embedded Configuration):**

.. code-block:: yaml

   apiVersion: volsync.backube/v1alpha1
   kind: ReplicationSource
   metadata:
     name: app-backup
     namespace: production
   spec:
     sourcePVC: app-data
     kopia:
       repository: prod-backup-config
       maintenanceCronJob:
         enabled: true
         schedule: "0 2 * * *"
         resources:
           requests:
             memory: "256Mi"

**After (Separate KopiaMaintenance):**

.. code-block:: yaml

   # Step 1: Create KopiaMaintenance resource
   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: prod-maintenance
     namespace: production
   spec:
     repository:
       repository: prod-backup-config
     schedule: "0 2 * * *"
     resources:
       requests:
         memory: "256Mi"
       limits:
         memory: "1Gi"

   ---
   # Step 2: Remove maintenanceCronJob from ReplicationSource
   apiVersion: volsync.backube/v1alpha1
   kind: ReplicationSource
   metadata:
     name: app-backup
     namespace: production
   spec:
     sourcePVC: app-data
     kopia:
       repository: prod-backup-config
       # maintenanceCronJob section removed

Migration Steps
----------------

1. **Create KopiaMaintenance resources** before modifying ReplicationSources
2. **Verify CronJob creation** using ``kubectl get cronjobs -n <namespace>``
3. **Remove embedded configuration** from ReplicationSources
4. **Monitor maintenance execution** to ensure continuity

Troubleshooting
===============

Common Issues
-------------

Maintenance Not Running
^^^^^^^^^^^^^^^^^^^^^^^

**Symptoms:**

- No CronJob created in namespace
- ``status.activeCronJob`` is empty

**Solutions:**

1. Verify repository secret exists:

   .. code-block:: bash

      kubectl get secret <repository-secret> -n <namespace>

2. Check KopiaMaintenance status:

   .. code-block:: bash

      kubectl describe kopiamaintenance <name> -n <namespace>

3. Review controller logs for errors:

   .. code-block:: bash

      kubectl logs -n volsync-system deployment/volsync | grep -i kopiamaintenance

Authentication Failures
^^^^^^^^^^^^^^^^^^^^^^^

**Symptoms:**

- Maintenance jobs fail with authentication errors
- Repository access denied messages

**Solutions:**

1. Verify secret contains required fields:

   .. code-block:: bash

      kubectl get secret <repository-secret> -n <namespace> -o jsonpath='{.data}' | jq 'keys'

2. Check secret data is valid and not corrupted
3. Ensure custom CA is properly configured if using self-signed certificates

Resource Exhaustion
^^^^^^^^^^^^^^^^^^^

**Symptoms:**

- Maintenance jobs killed or evicted
- Out of memory errors

**Solutions:**

1. Increase resource limits:

   .. code-block:: yaml

      resources:
        requests:
          memory: "1Gi"
        limits:
          memory: "4Gi"

2. Monitor actual usage:

   .. code-block:: bash

      kubectl top pod -n <namespace> -l job-name=<maintenance-job>

Schedule Not Working
^^^^^^^^^^^^^^^^^^^^

**Symptoms:**

- Jobs not running at expected times
- Incorrect execution frequency

**Solutions:**

1. Validate cron expression using online validators or tools
2. Check controller timezone configuration
3. Verify ``suspend`` is not set to ``true``

Debugging Commands
------------------

.. code-block:: bash

   # Check KopiaMaintenance resources
   kubectl get kopiamaintenance -A

   # View detailed status
   kubectl describe kopiamaintenance <name> -n <namespace>

   # Check created CronJobs
   kubectl get cronjobs -n <namespace> -l volsync.backube/kopia-maintenance=true

   # View maintenance job logs
   kubectl logs -n <namespace> job/<maintenance-job-name>

   # Check events for errors
   kubectl get events -n <namespace> --field-selector involvedObject.name=<maintenance-name>

Limitations
===========

Current Limitations
-------------------

1. **Namespace Isolation**: Repository secret must exist in the same namespace as KopiaMaintenance
2. **No Cross-Namespace Management**: Cannot manage repositories in different namespaces
3. **Single Repository**: Each KopiaMaintenance manages exactly one repository
4. **No Repository Discovery**: No automatic detection of repositories or ReplicationSources

Design Rationale
----------------

The simplified design provides:

- **Clear ownership**: Namespace-scoped resources have clear ownership boundaries
- **Better security**: No cross-namespace secret access reduces attack surface
- **Simpler RBAC**: Namespace-level permissions are easier to manage
- **Predictable behavior**: Direct configuration eliminates matching complexity

Next Steps
==========

- Review :doc:`backup-configuration` for repository setup
- Explore :doc:`troubleshooting` for detailed debugging
- Learn about :doc:`maintenance-schedule-conflicts` if managing multiple repositories
- Understand `Kopia's maintenance operations <https://kopia.io/docs/maintenance/>`_ in detail

Support
=======

For issues or questions:

- GitHub Issues: https://github.com/backube/volsync/issues
- GitHub Discussions: https://github.com/backube/volsync/discussions
- Documentation: https://volsync.readthedocs.io/