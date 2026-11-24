# Offline selected-state evidence

Covers T1 / S13 item 3, subject to S6 and S9. No live host package data,
package tools, container probes, Go builds or external processes were used.
No isolation or APT success is established. Original evidence was not changed.

## Public API and integration boundary

`host_integrity.host_hashes()` returns a JSON-serializable selected-state
snapshot with explicit scope, exclusions, limitations, counts, per-file SHA256,
symlink text, stable metadata and a canonical snapshot SHA256. Errors propagate
as OSError or ValueError (including UnstableEvidence). Parent integration into
runner.py and host_selected_state_unchanged is still required.

Required: /var/lib/dpkg/status and the complete /var/lib/dpkg/info tree.
Optional, with explicit absence: /var/lib/dpkg/status-old,
/var/lib/dpkg/available, /var/lib/apt/extended_states.
Locks, APT lists/partial/cache, credentials and all other paths are excluded;
excluded state is not attested. Info links attest only link text, never targets.
Two matching snapshots do not prove continuous integrity or detect restored
changes between observations or changes after capture. atime is omitted because
reads can change it. Constants are fixture-patchable, not CLI/env options.

## RED: old helper's controlled lock failure

Command (exit 1; stdout empty; stderr below):

```text
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s test/e2e/apt -p test_host_integrity.py -v
```

The initial test imported the existing runner.host_hashes and strictly mocked
Path.is_dir, os.walk, os.lstat and os.open. The sole sanitized inventory entry
was /var/lib/dpkg/lock; its mocked open raised PermissionError. Popen was denied.
This was a behavioral failure, not a missing-module import. The final tests no
longer import runner, avoiding dependence on concurrent parent integration.

```text
test_selected_evidence_does_not_open_dpkg_lock (test_host_integrity.LockRegression.test_selected_evidence_does_not_open_dpkg_lock) ... ERROR

======================================================================
ERROR: test_selected_evidence_does_not_open_dpkg_lock (test_host_integrity.LockRegression.test_selected_evidence_does_not_open_dpkg_lock)
----------------------------------------------------------------------
Traceback (most recent call last):
  File "/home/ace/.herdr/worktrees/codenburg-dots/feat-phase5-apt-e2e/test/e2e/apt/test_host_integrity.py", line 22, in test_selected_evidence_does_not_open_dpkg_lock
    self.assertNotIn("/var/lib/dpkg/lock", runner.host_hashes())
                                           ^^^^^^^^^^^^^^^^^^^^
  File "/home/ace/.herdr/worktrees/codenburg-dots/feat-phase5-apt-e2e/test/e2e/apt/runner.py", line 588, in host_hashes
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
         ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/usr/lib/python3.12/unittest/mock.py", line 1134, in __call__
    return self._mock_call(*args, **kwargs)
           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/usr/lib/python3.12/unittest/mock.py", line 1138, in _mock_call
    return self._execute_mock_call(*args, **kwargs)
           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/usr/lib/python3.12/unittest/mock.py", line 1199, in _execute_mock_call
    result = effect(*args, **kwargs)
             ^^^^^^^^^^^^^^^^^^^^^^^
  File "/home/ace/.herdr/worktrees/codenburg-dots/feat-phase5-apt-e2e/test/e2e/apt/test_host_integrity.py", line 16, in denied_open
    raise PermissionError("sanitized unreadable dpkg lock")
PermissionError: sanitized unreadable dpkg lock

----------------------------------------------------------------------
Ran 1 test in 0.005s

FAILED (errors=1)
```

## GREEN and self-review

Same exact command: initial implementation exit 0, stdout empty,
stderr ended `Ran 16 tests in 0.064s` / `OK`.
Self-review tightened wrong-type rejection before traversal, including a
file-to-directory selection race, and added four focused regressions.

Final same exact command: exit 0; stdout empty; stderr:

```text
test_directory_inventory_race_fails_closed (test_host_integrity.SelectedStateTests.test_directory_inventory_race_fails_closed) ... ok
test_disappearing_tree_child_is_not_optional_absence (test_host_integrity.SelectedStateTests.test_disappearing_tree_child_is_not_optional_absence) ... ok
test_file_change_during_read_fails_closed (test_host_integrity.SelectedStateTests.test_file_change_during_read_fails_closed) ... ok
test_hash_read_error_propagates (test_host_integrity.SelectedStateTests.test_hash_read_error_propagates) ... ok
test_lock_exclusion_and_complete_selected_inventory (test_host_integrity.SelectedStateTests.test_lock_exclusion_and_complete_selected_inventory) ... ok
test_missing_optional_parent_is_not_optional_absence (test_host_integrity.SelectedStateTests.test_missing_optional_parent_is_not_optional_absence) ... ok
test_missing_required_file_and_tree_fail_closed (test_host_integrity.SelectedStateTests.test_missing_required_file_and_tree_fail_closed) ... ok
test_optional_absence_change_between_snapshots_fails_closed (test_host_integrity.SelectedStateTests.test_optional_absence_change_between_snapshots_fails_closed) ... ok
test_optional_symlink_and_wrong_type_are_not_absence (test_host_integrity.SelectedStateTests.test_optional_symlink_and_wrong_type_are_not_absence) ... ok
test_present_optional_files_and_nested_empty_directories (test_host_integrity.SelectedStateTests.test_present_optional_files_and_nested_empty_directories) ... ok
test_selected_file_replaced_before_open_fails_closed (test_host_integrity.SelectedStateTests.test_selected_file_replaced_before_open_fails_closed) ... ok
test_selected_files_root_and_ancestors_reject_symlinks (test_host_integrity.SelectedStateTests.test_selected_files_root_and_ancestors_reject_symlinks) ... ok
test_selected_type_race_rejected_before_traversal (test_host_integrity.SelectedStateTests.test_selected_type_race_rejected_before_traversal) ... ok
test_symlink_change_during_readlink_fails_closed (test_host_integrity.SelectedStateTests.test_symlink_change_during_readlink_fails_closed) ... ok
test_tree_iteration_error_propagates (test_host_integrity.SelectedStateTests.test_tree_iteration_error_propagates) ... ok
test_tree_links_inventory_text_without_following_targets (test_host_integrity.SelectedStateTests.test_tree_links_inventory_text_without_following_targets) ... ok
test_tree_nonregular_file_fails_closed_without_blocking (test_host_integrity.SelectedStateTests.test_tree_nonregular_file_fails_closed_without_blocking) ... ok
test_tree_traversal_error_propagates (test_host_integrity.SelectedStateTests.test_tree_traversal_error_propagates) ... ok
test_unreadable_selected_file_propagates (test_host_integrity.SelectedStateTests.test_unreadable_selected_file_propagates) ... ok
test_wrong_selected_type_rejected_before_traversal (test_host_integrity.SelectedStateTests.test_wrong_selected_type_rejected_before_traversal) ... ok

----------------------------------------------------------------------
Ran 20 tests in 0.076s

OK
```

Preserved regular-file hashing, strict traversal/hash error propagation,
no-follow file opens and link-text inventory. Fixtures were created only in
unique private subdirectories here and cleaned by their owning tests; the
pre-existing evidence root was not removed. Tests deny every Popen spawn.

S6: offline only, fail-closed selected evidence. S9: no production changes,
unrelated refactoring or Git delivery. S13 item 3: scoped evidence and relevant
regressions complete; running-profile/cleanup corrections, full suite, independent
review and live isolation/E2E remain parent-owned and unclaimed.
