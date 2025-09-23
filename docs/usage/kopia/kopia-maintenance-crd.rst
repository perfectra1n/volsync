===============================
KopiaMaintenance CRD Reference
===============================

.. sidebar:: Contents

   .. contents:: KopiaMaintenance CRD
      :local:

Overview and Introduction
=========================

What is KopiaMaintenance?
-------------------------

The KopiaMaintenance Custom Resource Definition (CRD) is a cluster-scoped resource that provides centralized, flexible management of Kopia repository maintenance operations in VolSync. It represents a significant evolution from the previous embedded ``maintenanceCronJob`` configuration in ReplicationSources.

**Key Benefits Over Legacy Approach:**

- **Centralized Management**: Single point of control for maintenance across all namespaces
- **Advanced Matching**: Sophisticated repository and namespace selection capabilities
- **Priority-Based Resolution**: Automatic conflict resolution when multiple configurations target the same repository
- **Cross-Namespace Operations**: Seamless secret management across namespace boundaries
- **Enhanced Flexibility**: Support for both repository selector and direct repository modes
- **Better Resource Management**: Optimized CronJob creation and lifecycle management

Why KopiaMaintenance was Created
--------------------------------

The embedded ``maintenanceCronJob`` configuration in ReplicationSources had several limitations:

1. **Limited Flexibility**: Each ReplicationSource required its own maintenance configuration
2. **No Cross-Namespace Management**: Maintenance was confined to individual namespaces
3. **Conflict Resolution**: No systematic way to handle conflicting maintenance schedules
4. **Resource Duplication**: Multiple ReplicationSources using the same repository created duplicate maintenance CronJobs
5. **Complex Administration**: Difficult to manage maintenance policies at scale

The KopiaMaintenance CRD addresses these limitations by providing:

- Cluster-wide maintenance policy management
- Intelligent repository matching and deduplication
- Priority-based conflict resolution
- Cross-namespace secret management
- Centralized monitoring and observability

When to Use KopiaMaintenance
----------------------------

**Use KopiaMaintenance when:**

- Managing maintenance for multiple repositories across namespaces
- Implementing organization-wide maintenance policies
- Requiring advanced matching criteria (wildcards, labels, namespace selectors)
- Setting up maintenance for repositories without ReplicationSources
- Need priority-based conflict resolution for shared repositories
- Centralizing maintenance operations for better observability

**Continue using embedded maintenanceCronJob when:**

- Simple single-namespace setups with minimal maintenance requirements
- Legacy configurations that work well and don't require advanced features
- Gradual migration scenarios where immediate change isn't necessary

.. note::
   The embedded ``maintenanceCronJob`` configuration is not deprecated and continues to work.
   KopiaMaintenance provides additional capabilities for advanced use cases.

CRD Specification
=================

Complete Field Reference
------------------------

KopiaMaintenanceSpec Fields
^^^^^^^^^^^^^^^^^^^^^^^^^^^

**repositorySelector** (*KopiaRepositorySelector*, optional)
   Repository matching configuration for finding existing ReplicationSources.
   This approach matches ReplicationSources by their repository configuration.
   Either ``repositorySelector`` OR ``repository`` must be specified, but not both.

**repository** (*KopiaRepositorySpec*, optional)
   Repository defines a direct repository configuration for maintenance.
   This approach allows KopiaMaintenance to work independently of ReplicationSources.
   Either ``repositorySelector`` OR ``repository`` must be specified, but not both.

**schedule** (*string*, optional, default: "0 2 * * *")
   Cron schedule for when maintenance should run. The schedule is interpreted
   in the controller's timezone. Must match the pattern for valid cron expressions.

**enabled** (*bool*, optional, default: true)
   Determines if maintenance should be performed. When false, no maintenance
   will be scheduled.

**priority** (*int32*, optional, default: 0)
   Priority of this maintenance configuration. When multiple KopiaMaintenance
   resources match the same repository, the one with the highest priority wins.
   Range: -100 to 100.

**suspend** (*bool*, optional)
   Can be used to temporarily stop maintenance. When true, the CronJob will
   not create new Jobs, but existing Jobs will be allowed to complete.

**successfulJobsHistoryLimit** (*int32*, optional, default: 3)
   Specifies how many successful maintenance Jobs should be kept. Minimum: 0.

**failedJobsHistoryLimit** (*int32*, optional, default: 1)
   Specifies how many failed maintenance Jobs should be kept. Minimum: 0.

**resources** (*corev1.ResourceRequirements*, optional)
   Compute resources required by the maintenance container. If not specified,
   defaults to 256Mi memory request and 1Gi memory limit.

**serviceAccountName** (*string*, optional)
   Allows specifying a custom ServiceAccount for maintenance jobs. If not
   specified, a default maintenance ServiceAccount will be used.

**moverPodLabels** (*map[string]string*, optional)
   Labels that should be added to maintenance pods. These will be in addition
   to any labels that VolSync may add.

**nodeSelector** (*map[string]string*, optional)
   Node selector for maintenance pods.

**tolerations** (*[]corev1.Toleration*, optional)
   Tolerations for maintenance pods.

**affinity** (*corev1.Affinity*, optional)
   Affinity for maintenance pods.

Repository Selector Mode
^^^^^^^^^^^^^^^^^^^^^^^^

**KopiaRepositorySelector** allows matching existing ReplicationSources:

**repository** (*string*, optional)
   Name of the repository secret to match. Supports wildcards:
   - ``*`` matches any number of characters
   - ``?`` matches a single character
   - Examples: ``kopia-*``, ``backup-?-repo``, ``prod-*-backup``

**namespaceSelector** (*NamespaceSelector*, optional)
   Defines which namespaces to match. If not specified, matches all namespaces.

**customCA** (*CustomCASelector*, optional)
   Matches repositories using specific custom CA configuration.

**repositoryType** (*string*, optional)
   Matches specific repository types (e.g., "s3", "azure", "gcs", "filesystem").
   If not specified, matches all types.

**labels** (*map[string]string*, optional)
   Matches ReplicationSources with specific labels.

**NamespaceSelector** defines namespace selection criteria:

**matchNames** (*[]string*, optional)
   Lists specific namespace names to match.

**matchLabels** (*map[string]string*, optional)
   Matches namespaces by their labels.

**excludeNames** (*[]string*, optional)
   Lists namespace names to exclude.

**CustomCASelector** defines CA selection criteria:

**secretName** (*string*, optional)
   Matches repositories using this CA secret name. Supports wildcards.

**configMapName** (*string*, optional)
   Matches repositories using this CA ConfigMap name. Supports wildcards.

Direct Repository Mode
^^^^^^^^^^^^^^^^^^^^^^

**KopiaRepositorySpec** defines direct repository configuration:

**repository** (*string*, required)
   Secret name containing repository configuration. This secret should contain
   the repository connection details (URL, credentials, etc.) in the same
   format as used by ReplicationSources.

**namespace** (*string*, optional)
   Namespace where the repository secret is located. If not specified,
   defaults to the VolSync operator namespace.

**customCA** (*ReplicationSourceKopiaCA*, optional)
   Optional custom CA configuration for repository access.

**repositoryType** (*string*, optional)
   Specifies the type of repository (e.g., "s3", "azure", "gcs", "filesystem").
   This helps with validation and provides metadata for maintenance operations.

Status Fields
^^^^^^^^^^^^^

**KopiaMaintenanceStatus** provides observed state information:

**matchedSources** (*[]MatchedSource*, optional)
   Lists the ReplicationSources currently matched by this maintenance configuration.

**activeCronJobs** (*[]string*, optional)
   Lists the CronJobs currently managed by this maintenance configuration.

**lastReconcileTime** (*metav1.Time*, optional)
   Last time this maintenance configuration was reconciled.

**lastMaintenanceTime** (*metav1.Time*, optional)
   Last time maintenance was successfully performed.

**nextScheduledMaintenance** (*metav1.Time*, optional)
   Next scheduled maintenance time.

**maintenanceFailures** (*int32*, optional)
   Counts the number of consecutive maintenance failures.

**conditions** (*[]metav1.Condition*, optional)
   Represents the latest available observations of the maintenance
   configuration's state.

**conflictingMaintenances** (*[]string*, optional)
   Lists other KopiaMaintenance resources that match the same repositories
   but have lower priority.

**MatchedSource** represents a ReplicationSource matched by this maintenance:

**name** (*string*, required)
   Name of the ReplicationSource.

**namespace** (*string*, required)
   Namespace of the ReplicationSource.

**repository** (*string*, required)
   Repository being used.

**lastMatched** (*metav1.Time*, optional)
   When this source was last matched.

Validation Rules and Constraints
--------------------------------

**Mutual Exclusivity**
   Either ``repositorySelector`` OR ``repository`` must be specified, but not both.

**Required Fields**
   When using direct repository mode, the ``repository.repository`` field is required.

**Priority Range**
   Priority must be between -100 and 100 (inclusive).

**Schedule Format**
   Schedule must be a valid cron expression or supported alias (@daily, @weekly, etc.).

**Resource Limits**
   successfulJobsHistoryLimit and failedJobsHistoryLimit must be non-negative.

Configuration Examples
======================

Basic Repository Selector Example
----------------------------------

Match all Kopia repositories with names starting with "prod-":

.. code-block:: yaml

   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: prod-maintenance
   spec:
     repositorySelector:
       repository: "prod-*"
     schedule: "0 3 * * *"  # 3 AM daily
     priority: 10
     resources:
       requests:
         cpu: "200m"
         memory: "512Mi"
       limits:
         cpu: "500m"
         memory: "1Gi"

Basic Direct Repository Example
-------------------------------

Configure maintenance for a specific repository without requiring ReplicationSources:

.. code-block:: yaml

   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: direct-maintenance
   spec:
     repository:
       repository: kopia-backup-config
       namespace: backup-storage
       repositoryType: s3
     schedule: "0 2 * * 0"  # Weekly on Sunday at 2 AM
     enabled: true
     resources:
       requests:
         memory: "256Mi"
       limits:
         memory: "2Gi"

Advanced Wildcard Matching
---------------------------

Complex repository name pattern matching:

.. code-block:: yaml

   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: database-maintenance
   spec:
     repositorySelector:
       repository: "*-database-backup"  # Matches prod-database-backup, staging-database-backup
       repositoryType: s3
       labels:
         backup-type: database
         environment: production
     schedule: "0 1 * * 0"  # Weekly at 1 AM Sunday
     priority: 20
     successfulJobsHistoryLimit: 5
     failedJobsHistoryLimit: 2

Namespace Selector Configuration
--------------------------------

Target specific namespaces with exclusions:

.. code-block:: yaml

   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: production-maintenance
   spec:
     repositorySelector:
       repository: "kopia-*"
       namespaceSelector:
         matchNames:
           - prod-app-1
           - prod-app-2
           - prod-database
         excludeNames:
           - prod-test
         matchLabels:
           environment: production
           backup-enabled: "true"
     schedule: "0 4 * * *"  # 4 AM daily
     priority: 15

Priority-Based Conflict Resolution
----------------------------------

High-priority maintenance overrides lower priority configurations:

.. code-block:: yaml

   # High priority configuration for critical repositories
   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: critical-maintenance
   spec:
     repositorySelector:
       repository: "critical-*"
       labels:
         tier: critical
     schedule: "0 2 * * *"  # 2 AM daily
     priority: 50  # High priority
     resources:
       requests:
         cpu: "500m"
         memory: "1Gi"
       limits:
         cpu: "2"
         memory: "4Gi"

   ---
   # Lower priority configuration for general repositories
   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: general-maintenance
   spec:
     repositorySelector:
       repository: "*"  # Matches all
     schedule: "0 3 * * 0"  # Weekly at 3 AM
     priority: 0  # Default priority
     resources:
       requests:
         memory: "256Mi"
       limits:
         memory: "1Gi"

Custom CA Configuration
-----------------------

Repository selector with custom CA matching:

.. code-block:: yaml

   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: secure-maintenance
   spec:
     repositorySelector:
       repository: "secure-*"
       customCA:
         secretName: "company-ca-*"
         configMapName: "internal-ca"
     schedule: "0 5 * * *"  # 5 AM daily
     priority: 25

Direct repository with custom CA:

.. code-block:: yaml

   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: private-maintenance
   spec:
     repository:
       repository: private-s3-config
       namespace: secure-backups
       customCA:
         secretName: private-ca-cert
     schedule: "0 1 * * 1"  # Weekly on Monday at 1 AM

Cross-Namespace Secret References
---------------------------------

Access repository secrets from different namespaces:

.. code-block:: yaml

   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: cross-namespace-maintenance
   spec:
     repository:
       repository: shared-backup-config
       namespace: backup-infrastructure  # Different from maintenance namespace
     schedule: "0 6 * * *"  # 6 AM daily
     serviceAccountName: maintenance-cross-ns-sa
     resources:
       requests:
         cpu: "300m"
         memory: "512Mi"

Resource Limits and Node Selectors
----------------------------------

Advanced pod scheduling and resource management:

.. code-block:: yaml

   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: high-performance-maintenance
   spec:
     repositorySelector:
       repository: "large-data-*"
       repositoryType: s3
     schedule: "0 0 * * 0"  # Weekly at midnight Sunday
     priority: 30
     resources:
       requests:
         cpu: "1"
         memory: "2Gi"
       limits:
         cpu: "4"
         memory: "8Gi"
     nodeSelector:
       node-type: high-memory
       storage-type: ssd
     tolerations:
       - key: maintenance-only
         operator: Equal
         value: "true"
         effect: NoSchedule
     affinity:
       nodeAffinity:
         requiredDuringSchedulingIgnoredDuringExecution:
           nodeSelectorTerms:
             - matchExpressions:
                 - key: kubernetes.io/arch
                   operator: In
                   values: ["amd64"]

Temporary Maintenance Suspension
--------------------------------

Temporarily disable maintenance without deleting the configuration:

.. code-block:: yaml

   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: suspended-maintenance
   spec:
     repositorySelector:
       repository: "temp-*"
     schedule: "0 3 * * *"
     enabled: true
     suspend: true  # Temporarily suspend all maintenance
     successfulJobsHistoryLimit: 10  # Keep more history while suspended
     failedJobsHistoryLimit: 5

Migration Guide
===============

Migrating from Embedded maintenanceCronJob
-------------------------------------------

**Step 1: Assess Current Configuration**

Inventory your existing ReplicationSources with embedded maintenance:

.. code-block:: bash

   # Find all ReplicationSources with maintenance enabled
   kubectl get replicationsources --all-namespaces -o json | \
     jq -r '.items[] | select(.spec.kopia.maintenanceCronJob.enabled // true) |
     "\(.metadata.namespace)/\(.metadata.name): \(.spec.kopia.repository)"'

**Step 2: Identify Grouping Opportunities**

Look for opportunities to group repositories by:
- Repository patterns (same backup infrastructure)
- Namespace patterns (same team or environment)
- Maintenance schedule requirements
- Resource requirements

**Step 3: Create KopiaMaintenance Resources**

**Example migration for multiple ReplicationSources:**

*Before - Multiple ReplicationSources:*

.. code-block:: yaml

   # ReplicationSource 1
   apiVersion: volsync.backube/v1alpha1
   kind: ReplicationSource
   metadata:
     name: app1-backup
     namespace: production
   spec:
     sourcePVC: app1-data
     kopia:
       repository: prod-backup-config
       maintenanceCronJob:
         enabled: true
         schedule: "0 2 * * *"
         resources:
           requests:
             memory: "256Mi"

   ---
   # ReplicationSource 2
   apiVersion: volsync.backube/v1alpha1
   kind: ReplicationSource
   metadata:
     name: app2-backup
     namespace: production
   spec:
     sourcePVC: app2-data
     kopia:
       repository: prod-backup-config
       maintenanceCronJob:
         enabled: true
         schedule: "0 2 * * *"  # Same schedule
         resources:
           requests:
             memory: "256Mi"

*After - Single KopiaMaintenance:*

.. code-block:: yaml

   # Single KopiaMaintenance for all production backups
   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: production-maintenance
   spec:
     repositorySelector:
       repository: "prod-*"
       namespaceSelector:
         matchNames: ["production"]
     schedule: "0 2 * * *"
     resources:
       requests:
         memory: "256Mi"
       limits:
         memory: "1Gi"

   ---
   # Simplified ReplicationSources (maintenance removed)
   apiVersion: volsync.backube/v1alpha1
   kind: ReplicationSource
   metadata:
     name: app1-backup
     namespace: production
   spec:
     sourcePVC: app1-data
     kopia:
       repository: prod-backup-config
       # maintenanceCronJob removed - handled by KopiaMaintenance

   ---
   apiVersion: volsync.backube/v1alpha1
   kind: ReplicationSource
   metadata:
     name: app2-backup
     namespace: production
   spec:
     sourcePVC: app2-data
     kopia:
       repository: prod-backup-config
       # maintenanceCronJob removed - handled by KopiaMaintenance

Step-by-Step Migration Process
------------------------------

**1. Plan Your Migration**

- Document current maintenance schedules
- Identify repository sharing patterns
- Plan KopiaMaintenance resource names and priorities
- Schedule migration during maintenance windows

**2. Create KopiaMaintenance Resources**

Create your KopiaMaintenance resources before modifying ReplicationSources:

.. code-block:: bash

   # Apply KopiaMaintenance configurations
   kubectl apply -f kopia-maintenance-configs.yaml

**3. Verify Matching**

Confirm that your KopiaMaintenance resources are matching the intended ReplicationSources:

.. code-block:: bash

   # Check matched sources
   kubectl get kopiamaintenance -o yaml | \
     yq eval '.items[].status.matchedSources[]' -

**4. Remove Embedded Configuration**

Gradually remove embedded maintenanceCronJob configuration from ReplicationSources:

.. code-block:: bash

   # Remove maintenanceCronJob section from ReplicationSource
   kubectl patch replicationsource app1-backup -n production --type='json' \
     -p='[{"op": "remove", "path": "/spec/kopia/maintenanceCronJob"}]'

**5. Verify Migration**

Ensure maintenance continues working correctly:

.. code-block:: bash

   # Check that CronJobs are still created
   kubectl get cronjobs -n volsync-system -l volsync.backube/kopia-maintenance=true

   # Verify no duplicate CronJobs exist
   kubectl get cronjobs --all-namespaces | grep maintenance

Verifying Migration Success
---------------------------

**Check KopiaMaintenance Status:**

.. code-block:: bash

   # View all KopiaMaintenance resources and their status
   kubectl get kopiamaintenance -o wide

   # Check detailed status
   kubectl describe kopiamaintenance production-maintenance

**Verify CronJob Creation:**

.. code-block:: bash

   # Ensure CronJobs are created in volsync-system
   kubectl get cronjobs -n volsync-system \
     -l volsync.backube/kopia-maintenance=true \
     -o custom-columns=NAME:.metadata.name,SCHEDULE:.spec.schedule,SUSPEND:.spec.suspend

**Check Matched Sources:**

.. code-block:: bash

   # View which ReplicationSources are matched
   kubectl get kopiamaintenance production-maintenance \
     -o jsonpath='{.status.matchedSources[*].name}' | tr ' ' '\n'

Troubleshooting Migration Issues
--------------------------------

**Issue: KopiaMaintenance Not Matching Expected Sources**

*Symptoms:* status.matchedSources is empty or missing expected ReplicationSources.

*Solutions:*

1. Check repository name patterns:

   .. code-block:: bash

      # List actual repository names
      kubectl get replicationsources --all-namespaces \
        -o jsonpath='{.items[*].spec.kopia.repository}' | tr ' ' '\n' | sort -u

2. Verify namespace selector:

   .. code-block:: bash

      # Check namespace labels if using matchLabels
      kubectl get namespaces --show-labels

3. Test wildcard patterns:

   .. code-block:: bash

      # Debug pattern matching by checking controller logs
      kubectl logs -n volsync-system deployment/volsync | \
        grep -i "kopiamaintenance"

**Issue: Duplicate CronJobs After Migration**

*Symptoms:* Multiple CronJobs exist for the same repository.

*Solutions:*

1. Check for remaining embedded configurations:

   .. code-block:: bash

      # Find ReplicationSources still using embedded maintenance
      kubectl get replicationsources --all-namespaces -o json | \
        jq -r '.items[] | select(.spec.kopia.maintenanceCronJob) |
        "\(.metadata.namespace)/\(.metadata.name)"'

2. Remove old CronJobs if necessary:

   .. code-block:: bash

      # List old CronJobs outside volsync-system
      kubectl get cronjobs --all-namespaces | \
        grep -v volsync-system | grep maintenance

**Issue: Priority Conflicts Not Resolved**

*Symptoms:* Lower priority KopiaMaintenance is being used instead of higher priority.

*Solutions:*

1. Check conflicting maintenances:

   .. code-block:: bash

      # View conflicts
      kubectl get kopiamaintenance \
        -o jsonpath='{.items[*].status.conflictingMaintenances}' | tr ' ' '\n'

2. Verify priority values:

   .. code-block:: bash

      # List priorities
      kubectl get kopiamaintenance \
        -o custom-columns=NAME:.metadata.name,PRIORITY:.spec.priority

3. Force reconciliation:

   .. code-block:: bash

      # Trigger reconciliation by updating annotation
      kubectl annotate kopiamaintenance production-maintenance \
        volsync.backube/reconcile-trigger="$(date)" --overwrite

Best Practices
==============

Recommended Naming Conventions
------------------------------

**KopiaMaintenance Resource Names:**
- Use descriptive names that indicate scope: ``production-maintenance``, ``staging-databases``, ``critical-apps``
- Include environment or tier information: ``prod-high-priority``, ``dev-general``
- For repository-specific maintenance: ``s3-primary-maintenance``, ``gcs-backup-maintenance``

**Repository Secret Patterns:**
- Use consistent prefixes: ``kopia-prod-*``, ``backup-staging-*``
- Include repository type: ``s3-primary-config``, ``gcs-backup-config``
- Environment indicators: ``prod-app1-backup``, ``staging-db-backup``

**Label Strategies:**
- Environment: ``environment: production|staging|development``
- Tier: ``tier: critical|standard|dev``
- Team: ``team: platform|app-team-1|database-team``
- Backup type: ``backup-type: database|application|storage``

Organization Patterns
---------------------

**Hierarchical Approach:**

.. code-block:: yaml

   # High-priority critical systems
   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: critical-systems
   spec:
     repositorySelector:
       labels:
         tier: critical
     priority: 50
     schedule: "0 1 * * *"  # 1 AM daily

   ---
   # Medium-priority production systems
   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: production-standard
   spec:
     repositorySelector:
       labels:
         environment: production
         tier: standard
     priority: 25
     schedule: "0 2 * * 0"  # Weekly

   ---
   # Low-priority development systems
   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: development-general
   spec:
     repositorySelector:
       namespaceSelector:
         matchLabels:
           environment: development
     priority: 0
     schedule: "0 3 * * 0"  # Weekly

**Environment-Based Approach:**

.. code-block:: yaml

   # Production maintenance - high frequency, high resources
   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: production-maintenance
   spec:
     repositorySelector:
       namespaceSelector:
         matchLabels:
           environment: production
     schedule: "0 2 * * *"  # Daily
     resources:
       requests:
         cpu: "500m"
         memory: "1Gi"
       limits:
         cpu: "2"
         memory: "4Gi"

   ---
   # Staging maintenance - medium frequency
   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: staging-maintenance
   spec:
     repositorySelector:
       namespaceSelector:
         matchLabels:
           environment: staging
     schedule: "0 3 * * 1,4"  # Monday and Thursday

Security Considerations
-----------------------

**Secret Access:**
- KopiaMaintenance requires read access to repository secrets across namespaces
- Secrets are automatically copied to the operator namespace with controlled access
- Original secrets remain in source namespaces maintaining isolation

**Service Account Configuration:**

.. code-block:: yaml

   # Custom service account with minimal permissions
   apiVersion: v1
   kind: ServiceAccount
   metadata:
     name: kopia-maintenance-sa
     namespace: volsync-system
   ---
   apiVersion: rbac.authorization.k8s.io/v1
   kind: ClusterRole
   metadata:
     name: kopia-maintenance-role
   rules:
     - apiGroups: [""]
       resources: ["secrets"]
       verbs: ["get", "list"]
       # Only for maintenance-related secrets
   ---
   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: secure-maintenance
   spec:
     repositorySelector:
       repository: "secure-*"
     serviceAccountName: kopia-maintenance-sa

**Network Policies:**
- Consider network policies for maintenance pods
- Ensure access to repository endpoints
- Restrict unnecessary network access

Performance Considerations
--------------------------

**Resource Planning:**

.. code-block:: yaml

   # High-performance maintenance for large repositories
   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: large-repository-maintenance
   spec:
     repositorySelector:
       repository: "large-data-*"
     schedule: "0 1 * * 0"  # Weekly during low usage
     resources:
       requests:
         cpu: "1"
         memory: "2Gi"
         # ephemeral-storage for temp files
       limits:
         cpu: "4"
         memory: "8Gi"
     nodeSelector:
       storage-type: "ssd"
       node-class: "high-memory"

**Schedule Optimization:**
- Stagger maintenance schedules to avoid resource conflicts
- Use weekly schedules for large repositories
- Consider repository size and network bandwidth

**Repository Deduplication:**
- Single KopiaMaintenance can serve multiple ReplicationSources
- Reduces resource consumption and CronJob proliferation
- Better for repositories shared across namespaces

Multi-Tenant Deployment Patterns
--------------------------------

**Tenant Isolation Pattern:**

.. code-block:: yaml

   # Tenant A maintenance
   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: tenant-a-maintenance
   spec:
     repositorySelector:
       namespaceSelector:
         matchLabels:
           tenant: tenant-a
       repository: "*"
     schedule: "0 2 * * *"
     priority: 10
     resources:
       requests:
         memory: "512Mi"
       limits:
         memory: "2Gi"

   ---
   # Tenant B maintenance with different schedule
   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: tenant-b-maintenance
   spec:
     repositorySelector:
       namespaceSelector:
         matchLabels:
           tenant: tenant-b
       repository: "*"
     schedule: "0 3 * * *"  # Different time
     priority: 10
     resources:
       requests:
         memory: "256Mi"
       limits:
         memory: "1Gi"

**Shared Infrastructure Pattern:**

.. code-block:: yaml

   # Shared maintenance for common repositories
   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: shared-infrastructure
   spec:
     repositorySelector:
       repository: "shared-*"
       labels:
         infrastructure: shared
     schedule: "0 4 * * 0"  # Weekly
     priority: 15

   ---
   # Tenant-specific override for critical workloads
   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: tenant-critical-override
   spec:
     repositorySelector:
       repository: "shared-critical-*"
       labels:
         tenant: premium
         tier: critical
     schedule: "0 1 * * *"  # Daily
     priority: 25  # Higher priority overrides shared maintenance

Troubleshooting
===============

Common Error Scenarios
----------------------

**KopiaMaintenance Not Creating CronJobs**

*Symptoms:*
- No CronJobs appear in volsync-system namespace
- status.activeCronJobs is empty

*Troubleshooting:*

.. code-block:: bash

   # Check KopiaMaintenance status
   kubectl describe kopiamaintenance <name>

   # Verify controller logs
   kubectl logs -n volsync-system deployment/volsync | \
     grep -i kopiamaintenance

   # Check validation errors
   kubectl get kopiamaintenance <name> -o yaml | \
     yq eval '.status.conditions[] | select(.type == "Ready")' -

*Common Causes:*
- Invalid repository selector patterns
- Missing repository secrets
- Validation errors in spec

**Repository Matching Issues**

*Symptoms:*
- Expected ReplicationSources not appearing in status.matchedSources
- Maintenance not running for intended repositories

*Debugging:*

.. code-block:: bash

   # List all repository names to verify patterns
   kubectl get replicationsources --all-namespaces \
     -o jsonpath='{.items[*].spec.kopia.repository}' | tr ' ' '\n' | sort -u

   # Check namespace labels if using namespaceSelector.matchLabels
   kubectl get namespaces --show-labels

   # Test wildcard patterns manually
   # If pattern is "prod-*", verify names like "prod-app1", "prod-db"

   # Check ReplicationSource labels if using repositorySelector.labels
   kubectl get replicationsources --all-namespaces --show-labels

*Solutions:*
- Adjust wildcard patterns to match actual repository names
- Verify namespace selectors against actual namespace labels/names
- Check that ReplicationSources have expected labels

**Secret Reference Problems**

*Symptoms:*
- Maintenance jobs fail with authentication errors
- CronJobs exist but Jobs fail

*Troubleshooting:*

.. code-block:: bash

   # Check if source secret exists
   kubectl get secret <repository-secret> -n <source-namespace>

   # Verify copied secret in volsync-system
   kubectl get secrets -n volsync-system | grep maintenance-

   # Check secret content (be careful with credentials)
   kubectl get secret <repository-secret> -n <source-namespace> \
     -o jsonpath='{.data}' | jq 'keys'

   # Check maintenance job logs
   kubectl logs -n volsync-system job/<maintenance-job-name>

*Solutions:*
- Ensure repository secret exists and has correct content
- Verify VolSync operator has permission to read secrets across namespaces
- Check that secret contains all required fields for repository type

**Schedule Conflicts and Resolution**

*Symptoms:*
- Maintenance running at unexpected times
- Multiple KopiaMaintenance resources targeting same repository

*Investigating:*

.. code-block:: bash

   # Check conflicting maintenances
   kubectl get kopiamaintenance \
     -o jsonpath='{.items[*].status.conflictingMaintenances}' | tr ' ' '\n'

   # View priorities and schedules
   kubectl get kopiamaintenance \
     -o custom-columns=NAME:.metadata.name,PRIORITY:.spec.priority,SCHEDULE:.spec.schedule

   # Check which KopiaMaintenance won conflict resolution
   kubectl get cronjobs -n volsync-system \
     -l volsync.backube/kopia-maintenance=true \
     -o custom-columns=NAME:.metadata.name,SCHEDULE:.spec.schedule

*Understanding Resolution:*
- Highest priority wins (higher number = higher priority)
- If priorities are equal, first-created wins
- Conflicts are recorded in status.conflictingMaintenances

**Cross-Namespace Secret Access Issues**

*Symptoms:*
- Maintenance jobs fail with "secret not found" errors
- Direct repository mode not working

*Debugging:*

.. code-block:: bash

   # Check if secret exists in specified namespace
   kubectl get secret <secret-name> -n <specified-namespace>

   # Verify VolSync operator permissions
   kubectl auth can-i get secrets --as=system:serviceaccount:volsync-system:volsync \
     -n <target-namespace>

   # Check operator logs for permission errors
   kubectl logs -n volsync-system deployment/volsync | \
     grep -i "permission\|forbidden\|unauthorized"

*Solutions:*
- Ensure secret exists in specified namespace
- Verify operator has cluster-wide secret read permissions
- Check namespace exists and is accessible

How to Debug Matching Issues
-----------------------------

**Step 1: Enable Verbose Logging**

.. code-block:: bash

   # Enable debug logging for VolSync operator
   kubectl patch deployment volsync -n volsync-system \
     -p '{"spec":{"template":{"spec":{"containers":[{"name":"manager","env":[{"name":"LOG_LEVEL","value":"debug"}]}]}}}}'

**Step 2: Check Repository Patterns**

.. code-block:: bash

   # Create a test script to validate patterns
   cat << 'EOF' > test-patterns.sh
   #!/bin/bash

   PATTERN="$1"

   # Get all repository names
   kubectl get replicationsources --all-namespaces -o json | \
     jq -r '.items[].spec.kopia.repository' | sort -u | while read repo; do
     # Simple pattern matching logic (extend as needed)
     if [[ "$repo" == $PATTERN ]]; then
       echo "MATCH: $repo"
     else
       echo "NO MATCH: $repo"
     fi
   done
   EOF

   chmod +x test-patterns.sh
   ./test-patterns.sh "prod-*"

**Step 3: Validate Namespace Selectors**

.. code-block:: bash

   # Check namespace labels
   kubectl get namespaces -o custom-columns=NAME:.metadata.name,LABELS:.metadata.labels

   # Test namespace selector logic
   MATCH_LABELS='{"environment":"production","backup-enabled":"true"}'
   kubectl get namespaces -l environment=production,backup-enabled=true

**Step 4: Test Complete Matching Logic**

.. code-block:: bash

   # Create debug KopiaMaintenance with verbose matching
   cat << EOF | kubectl apply -f -
   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: debug-matching
   spec:
     repositorySelector:
       repository: "*"  # Match all to see what's available
     enabled: false  # Don't actually run maintenance
     schedule: "0 0 * * 0"  # Weekly
   EOF

   # Check what gets matched
   kubectl get kopiamaintenance debug-matching \
     -o jsonpath='{.status.matchedSources[*].name}' | tr ' ' '\n'

**Step 5: Clean Up Debug Resources**

.. code-block:: bash

   # Remove debug resources
   kubectl delete kopiamaintenance debug-matching

   # Reset logging level if needed
   kubectl patch deployment volsync -n volsync-system \
     -p '{"spec":{"template":{"spec":{"containers":[{"name":"manager","env":[{"name":"LOG_LEVEL","value":"info"}]}]}}}}'

Performance Issues
------------------

**Maintenance Jobs Taking Too Long**

*Symptoms:*
- Maintenance jobs run for hours
- Jobs killed due to resource limits
- Backup operations blocked by long-running maintenance

*Solutions:*

.. code-block:: yaml

   # Increase resources for large repositories
   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: high-resource-maintenance
   spec:
     repositorySelector:
       repository: "large-repo-*"
     schedule: "0 1 * * 0"  # Weekly during low usage
     resources:
       requests:
         cpu: "1"
         memory: "4Gi"
       limits:
         cpu: "4"
         memory: "16Gi"
     nodeSelector:
       storage-type: "nvme"  # Fast storage
     tolerations:
       - key: maintenance-only
         operator: Equal
         value: "true"
         effect: NoSchedule

**Multiple Maintenance Jobs Running Simultaneously**

*Symptoms:*
- High resource usage during maintenance windows
- Jobs failing due to resource contention

*Solutions:*

.. code-block:: yaml

   # Stagger maintenance schedules
   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: repo-group-1
   spec:
     repositorySelector:
       repository: "group1-*"
     schedule: "0 1 * * 0"  # Sunday 1 AM

   ---
   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: repo-group-2
   spec:
     repositorySelector:
       repository: "group2-*"
     schedule: "0 3 * * 0"  # Sunday 3 AM (2 hours later)

**Resource Quota Exceeded**

*Symptoms:*
- Maintenance jobs fail to start
- "resource quota exceeded" errors

*Solutions:*

.. code-block:: bash

   # Check resource quotas in volsync-system
   kubectl get resourcequotas -n volsync-system

   # Check current resource usage
   kubectl top pods -n volsync-system

   # Adjust resource requests or increase quotas
   kubectl patch kopiamaintenance large-maintenance \
     --type='merge' -p='{"spec":{"resources":{"requests":{"memory":"1Gi"}}}}'

Reference Information
=====================

Complete API Reference
----------------------

**Resource Scope:** Cluster-scoped (no namespace)

**API Version:** volsync.backube/v1alpha1

**Kind:** KopiaMaintenance

**Shortname:** km

**Categories:** volsync

**Printer Columns:**
- Enabled (boolean)
- Schedule (string)
- Priority (integer)
- Matched (integer) - Number of matched sources
- Last Maintenance (date-time)
- Age (date)

**Finalizers:**
- volsync.backube/kopiamaintenance-finalizer

**Labels (added by controller):**
- volsync.backube/kopia-maintenance: "true"
- volsync.backube/repository-hash: <hash>

**Annotations (added by controller):**
- volsync.backube/last-reconcile: <timestamp>
- volsync.backube/schedule-conflict: <conflicting-schedules>

RBAC Requirements
-----------------

**Cluster Roles Required:**

.. code-block:: yaml

   # Core KopiaMaintenance permissions
   apiVersion: rbac.authorization.k8s.io/v1
   kind: ClusterRole
   metadata:
     name: kopiamaintenance-manager
   rules:
     # KopiaMaintenance resources
     - apiGroups: ["volsync.backube"]
       resources: ["kopiaMaintenances"]
       verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
     - apiGroups: ["volsync.backube"]
       resources: ["kopiaMaintenances/status"]
       verbs: ["get", "update", "patch"]
     - apiGroups: ["volsync.backube"]
       resources: ["kopiaMaintenances/finalizers"]
       verbs: ["update"]

     # ReplicationSource access for matching
     - apiGroups: ["volsync.backube"]
       resources: ["replicationSources"]
       verbs: ["get", "list", "watch"]

     # CronJob management
     - apiGroups: ["batch"]
       resources: ["cronjobs"]
       verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
     - apiGroups: ["batch"]
       resources: ["jobs"]
       verbs: ["get", "list", "watch"]

     # Secret access for repository configuration
     - apiGroups: [""]
       resources: ["secrets"]
       verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]

     # Namespace access for namespace selectors
     - apiGroups: [""]
       resources: ["namespaces"]
       verbs: ["get", "list", "watch"]

     # ConfigMap access for custom CA
     - apiGroups: [""]
       resources: ["configmaps"]
       verbs: ["get", "list", "watch"]

     # Events for status reporting
     - apiGroups: [""]
       resources: ["events"]
       verbs: ["create", "patch"]

**Service Account for Maintenance Jobs:**

.. code-block:: yaml

   apiVersion: v1
   kind: ServiceAccount
   metadata:
     name: kopia-maintenance
     namespace: volsync-system
   ---
   apiVersion: rbac.authorization.k8s.io/v1
   kind: ClusterRole
   metadata:
     name: kopia-maintenance-runner
   rules:
     # Minimal permissions for maintenance operations
     - apiGroups: [""]
       resources: ["secrets"]
       verbs: ["get"]
       resourceNames: ["maintenance-*"]  # Only copied maintenance secrets

Supported Repository Types
--------------------------

**Cloud Storage:**
- Amazon S3 and S3-compatible (MinIO, etc.)
- Google Cloud Storage (GCS)
- Microsoft Azure Blob Storage
- Backblaze B2
- Wasabi
- DigitalOcean Spaces

**Network Storage:**
- SFTP
- WebDAV
- Rclone (via config)

**Local Storage:**
- Filesystem (via PVC) - ReplicationSource only

**Repository Type Configuration:**

.. code-block:: yaml

   # S3 repository
   spec:
     repository:
       repository: s3-backup-config
       repositoryType: s3  # Optional but recommended

   # Or in selector mode
   spec:
     repositorySelector:
       repositoryType: s3  # Match only S3 repositories

**Environment Variables by Repository Type:**

*S3:*
- KOPIA_REPOSITORY
- KOPIA_PASSWORD
- AWS_ACCESS_KEY_ID / KOPIA_S3_ACCESS_KEY_ID
- AWS_SECRET_ACCESS_KEY / KOPIA_S3_SECRET_ACCESS_KEY
- AWS_S3_ENDPOINT / KOPIA_S3_ENDPOINT

*GCS:*
- KOPIA_REPOSITORY
- KOPIA_PASSWORD
- GOOGLE_APPLICATION_CREDENTIALS / KOPIA_GCS_CREDENTIALS_FILE

*Azure:*
- KOPIA_REPOSITORY
- KOPIA_PASSWORD
- AZURE_STORAGE_ACCOUNT / KOPIA_AZURE_STORAGE_ACCOUNT
- AZURE_STORAGE_KEY / KOPIA_AZURE_STORAGE_KEY

Limitations and Known Issues
----------------------------

**Current Limitations:**

1. **Repository Scope:** Direct repository mode cannot use repositoryPVC (filesystem repositories)
2. **Namespace Isolation:** While secrets are copied securely, all maintenance runs in volsync-system namespace
3. **Schedule Conflict Resolution:** Only priority-based; no automatic schedule adjustment
4. **Wildcard Matching:** Limited to simple patterns (* and ?); no full regex support
5. **Cross-Cluster:** KopiaMaintenance is cluster-scoped and cannot manage repositories in other clusters

**Known Issues:**

1. **Secret Synchronization Delays:** Changes to source secrets may take up to the reconciliation interval to propagate
2. **Namespace Label Changes:** Changes to namespace labels don't immediately trigger KopiaMaintenance reconciliation
3. **Priority Tie-Breaking:** When priorities are equal, creation timestamp determines precedence (may not be intuitive)

**Performance Considerations:**

1. **Resource Planning:** All maintenance jobs run in volsync-system; plan namespace resource quotas accordingly
2. **Network Bandwidth:** Maintenance operations can consume significant bandwidth for large repositories
3. **Storage I/O:** Maintenance operations are I/O intensive; consider node selection and storage performance

**Workarounds for Limitations:**

.. code-block:: yaml

   # For filesystem repositories, use ReplicationSource maintenance
   apiVersion: volsync.backube/v1alpha1
   kind: ReplicationSource
   metadata:
     name: filesystem-backup
   spec:
     sourcePVC: data
     kopia:
       repositoryPVC: backup-storage  # Not supported in KopiaMaintenance
       maintenanceCronJob:            # Use embedded configuration instead
         enabled: true
         schedule: "0 2 * * *"

   ---
   # For complex scheduling, use multiple KopiaMaintenance with different priorities
   apiVersion: volsync.backube/v1alpha1
   kind: KopiaMaintenance
   metadata:
     name: business-hours-maintenance
   spec:
     repositorySelector:
       repository: "prod-*"
       labels:
         maintenance-window: business-hours
     schedule: "0 12 * * 1-5"  # Noon on weekdays
     priority: 30

**Future Enhancements (Roadmap):**

- Full regex support for repository matching
- Cross-cluster maintenance management
- Advanced scheduling with automatic conflict resolution
- Repository health monitoring and alerting
- Maintenance operation metrics and dashboards
- Integration with external maintenance scheduling systems

Support and Community
---------------------

**Documentation:**
- Main documentation: https://volsync.readthedocs.io/
- Kopia documentation: https://kopia.io/docs/
- GitHub repository: https://github.com/backube/volsync

**Community:**
- GitHub Discussions: https://github.com/backube/volsync/discussions
- GitHub Issues: https://github.com/backube/volsync/issues
- Kubernetes Slack: #volsync channel

**Contributing:**
- Contributing Guide: https://github.com/backube/volsync/blob/main/CONTRIBUTING.md
- Development Setup: https://volsync.readthedocs.io/en/latest/installation/development.html

This concludes the comprehensive KopiaMaintenance CRD documentation. The KopiaMaintenance CRD provides powerful, flexible maintenance management capabilities that scale from simple single-repository setups to complex multi-tenant environments with advanced matching criteria and conflict resolution.