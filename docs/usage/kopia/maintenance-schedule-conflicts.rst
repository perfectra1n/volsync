====================================
Maintenance Schedule Conflict Resolution
====================================

When multiple ReplicationSources use the same Kopia repository, they may specify different maintenance schedules
in their ``maintenanceCronJob`` configurations. This document explains how VolSync resolves these conflicts.

Overview
--------

VolSync uses a centralized approach for Kopia maintenance, creating a single CronJob per repository
in the operator namespace, regardless of how many ReplicationSources use that repository. This approach:

* Reduces resource usage
* Simplifies management
* Ensures consistent maintenance across all users of a repository

Conflict Resolution Strategy: First-Wins
-----------------------------------------

When multiple ReplicationSources specify different maintenance schedules for the same repository,
VolSync uses a **first-wins** strategy:

1. **First source sets the schedule**: The first ReplicationSource to create a maintenance CronJob
   for a repository determines the schedule.

2. **Different namespaces cannot override**: Subsequent ReplicationSources from *different* namespaces
   cannot change the established schedule.

3. **Same namespace can update**: ReplicationSources from the *same* namespace can update the schedule,
   supporting single-tenant scenarios.

4. **Schedule persists after deletion**: The maintenance schedule persists even if the original
   ReplicationSource is deleted, as long as any other source still uses the repository.

5. **Conflicts are tracked**: Schedule conflicts are recorded in the CronJob's annotations for visibility.

Example Scenarios
-----------------

Scenario 1: Multiple Namespaces, Different Schedules
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

.. code-block:: yaml

   # namespace: team-a
   apiVersion: volsync.backube/v1alpha1
   kind: ReplicationSource
   metadata:
     name: source-a
     namespace: team-a
   spec:
     kopia:
       repository: shared-repo-secret
       maintenanceCronJob:
         enabled: true
         schedule: "0 2 * * *"  # 2 AM daily - This will be used

   ---
   # namespace: team-b
   apiVersion: volsync.backube/v1alpha1
   kind: ReplicationSource
   metadata:
     name: source-b
     namespace: team-b
   spec:
     kopia:
       repository: shared-repo-secret
       maintenanceCronJob:
         enabled: true
         schedule: "0 4 * * *"  # 4 AM daily - This will be IGNORED

**Result**: The maintenance CronJob runs at 2 AM (first source wins). Team-b's schedule is ignored,
and a conflict annotation is added to the CronJob.

Scenario 2: Same Namespace Updates
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

When multiple ReplicationSources in the same namespace use the same repository, the latest
configuration takes precedence:

.. code-block:: yaml

   # Both in namespace: my-app
   apiVersion: volsync.backube/v1alpha1
   kind: ReplicationSource
   metadata:
     name: source-1
     namespace: my-app
   spec:
     kopia:
       repository: app-repo-secret
       maintenanceCronJob:
         schedule: "0 2 * * *"  # Initial schedule

   ---
   # Later update in same namespace
   apiVersion: volsync.backube/v1alpha1
   kind: ReplicationSource
   metadata:
     name: source-2
     namespace: my-app
   spec:
     kopia:
       repository: app-repo-secret
       maintenanceCronJob:
         schedule: "0 3 * * *"  # Updated schedule - This WILL be applied

**Result**: The schedule is updated to 3 AM because both sources are in the same namespace.

Viewing Conflict Information
-----------------------------

To check if there are schedule conflicts for a maintenance CronJob:

.. code-block:: bash

   # List maintenance CronJobs in the operator namespace
   kubectl get cronjobs -n volsync-system -l volsync.backube/kopia-maintenance=true

   # Check for conflict annotations
   kubectl describe cronjob -n volsync-system <cronjob-name>

The annotation ``volsync.backube/schedule-conflict`` will contain information about the last
rejected schedule change attempt:

.. code-block:: yaml

   annotations:
     volsync.backube/schedule-conflict: |
       Last conflict: Schedule '0 4 * * *' requested from namespace 'team-b'
       at 2024-01-15T10:30:00Z (rejected - first-wins strategy)

Best Practices
--------------

1. **Coordinate schedules**: Teams sharing a repository should coordinate on maintenance schedules
   before deployment.

2. **Use dedicated repositories**: When different schedules are required, consider using separate
   Kopia repositories.

3. **Monitor conflicts**: Regularly check for schedule conflict annotations to identify and resolve
   coordination issues.

4. **Document agreements**: Document agreed-upon maintenance schedules for shared repositories in
   your team's documentation.

Disabling Maintenance
---------------------

If you don't want to participate in maintenance for a shared repository, you can disable it for
your ReplicationSource:

.. code-block:: yaml

   spec:
     kopia:
       repository: shared-repo-secret
       maintenanceCronJob:
         enabled: false  # This source won't affect maintenance scheduling

Alternative Approaches
----------------------

If the first-wins strategy doesn't meet your needs, consider:

1. **Separate repositories**: Use different Kopia repositories for different teams/applications
   that require different maintenance schedules.

2. **External maintenance**: Disable VolSync maintenance and manage Kopia maintenance externally
   using your own scheduling system.

3. **Consensus scheduling**: Agree on a common maintenance schedule that works for all users of
   the shared repository.

Technical Details
-----------------

* Maintenance CronJobs are created in the operator namespace (typically ``volsync-system``)
* Repository identity is determined by hashing the repository secret name and CA configuration
* The hash excludes namespace and schedule, ensuring one CronJob per unique repository
* Labels track which namespaces use each CronJob
* The CronJob persists until no ReplicationSources reference the repository