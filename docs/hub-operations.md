# Operating and recovering a team report hub

This runbook covers a single Linux host running the experimental report hub.
It is an operations foundation for a self-hosted pilot, not a managed service,
high-availability design, uptime commitment, or independently audited security
guarantee. The diagnostic engine still runs in CI; the hub stores summaries.

## Deployment boundary

Use a dedicated account, private local storage, and a TLS reverse proxy on the
same host. Keep port 8090 on loopback. Give the proxy explicit request-size,
connection and rate limits, and place the deployment behind your organization's
authenticated network perimeter. Neither the proxy nor monitoring should log
Authorization headers. Use a separately managed encrypted host volume if you
need encryption at rest: age backups do not encrypt the running database.
When GitHub OIDC publishing is configured, allow the hub's outbound HTTPS
requests to `token.actions.githubusercontent.com` for signing-key verification.
The service still listens only on loopback; restrict egress at the deployment
perimeter according to the enabled publisher features.

Build the hub from a reviewed, tested commit with `make build-hub`. The
[systemd unit](../deploy/hub/worldbisect-hub.service) is an installable template
for a systemd Linux host. Review its paths and resource limits before enabling
it; the sandbox directives require support from the host's systemd and kernel.
This repository's local restore drill does not certify every systemd setting
on every distribution.

```bash
sudo useradd --system --home-dir /var/lib/worldbisect-hub --shell /usr/sbin/nologin worldbisect-hub
sudo install -m 0755 bin/worldbisect-hub /usr/local/bin/worldbisect-hub
sudo install -d -m 0700 -o worldbisect-hub -g worldbisect-hub /etc/worldbisect-hub /var/lib/worldbisect-hub
sudo -u worldbisect-hub /usr/local/bin/worldbisect-hub init --config /etc/worldbisect-hub/config.json
sudo install -m 0644 deploy/hub/worldbisect-hub.service /etc/systemd/system/worldbisect-hub.service
sudo systemd-analyze verify /etc/systemd/system/worldbisect-hub.service
sudo systemctl daemon-reload
sudo systemctl enable --now worldbisect-hub
```

Capture initialization credentials directly into an approved secret manager.
Do not put them in the unit file, command-line arguments, logs, source control,
or screenshots. Give CI a separate write credential and reviewers read access.
The unit allows writes only to the service state directory; configuration is
read-only inside the running service's mount namespace.

## Establish the operating baseline

- Poll loopback `GET /healthz` for process liveness. A successful response alone
  does not prove that report persistence, retention, or an audit append works.
- Use a dedicated low-volume synthetic workspace to publish, read and delete a
  harmless report periodically. Authenticate through the same reverse proxy
  used by CI. Do not use a real customer workspace for this probe.
- Alert on sustained unexpected 5xx responses, failed retention/audit writes,
  repeated restarts, disk errors and low free space/inodes. Inspect
  `journalctl -u worldbisect-hub`; exclude submitted summaries and credentials
  from external logs.
- Treat 401/403 as access failures, 409 as a quota event and 429 as rate limiting.
  Do not blindly retry a POST whose outcome is unknown; check for the report
  first. Set retry/error policy according to the API version being deployed.
- Set monitoring thresholds from your own measured pilot workload. The example
  unit's 512 MiB memory limit and 64-task limit are bounds, not capacity claims.
- Track successful encrypted backup age and the last successful restore drill.
  The age of the last backup determines the possible summary data loss. A
  restoration must also include the corresponding configuration and audit data.

## Encrypted backup prerequisites

The optional [backup script](../scripts/hub-backup.py) requires Linux,
Python 3.10+, and **age / age-keygen**. It adds no cryptographic dependency to
the hub or causal engine. The development drill uses age **v1.3.2**. Install
the maintained age package supplied by your distribution or verify an upstream
release using its published verification instructions. A pinned development
installation, outside this repository's Go module, is:

```bash
GOBIN="$HOME/.local/bin" go install filippo.io/age/cmd/...@v1.3.2
age --version
```

See the [upstream age installation and verification instructions](https://github.com/FiloSottile/age#installation).
Use the official tools to generate an identity in a private location:

```bash
umask 077
age-keygen -o /secure/offline/hub-backup-identity.txt
age-keygen -y /secure/offline/hub-backup-identity.txt
```

The second command prints the public recipient. Store the secret identity in
an offline secret manager, separate from the hub host and backup storage. The
backup host needs only the public `age1...` recipient. This wrapper accepts
native age recipients and unencrypted native identity files; interactive
passphrases, SSH identities and age plugins are deliberately outside its scope.
Protect an exported identity file with mode 0600 and remove it after recovery.

Age provides ciphertext integrity and confidentiality. It does not authenticate
who produced a backup: anyone who knows the public recipient can encrypt a
different archive. Restrict writes to backup storage and retain a trusted
inventory of backup creation times and object identifiers. Manifest checksums
detect missing or changed archive members after successful decryption; they are
not digital signatures or a replacement for audit-chain verification.

## Create an offline backup

Stop the server and pause administrative configuration changes for the snapshot.
The script acquires the same exclusive locks as the report and audit stores and
rejects a running writer. It never stops the service itself. Configuration,
the complete data directory, and the complete audit directory belong to one
snapshot. Existing `.hub.lock` and `.audit.lock` files are excluded.

```bash
sudo install -d -m 0700 -o worldbisect-hub -g worldbisect-hub /var/backups/worldbisect-hub
sudo systemctl stop worldbisect-hub
sudo -u worldbisect-hub python3 scripts/hub-backup.py backup \
  --config /etc/worldbisect-hub/config.json \
  --data /var/lib/worldbisect-hub/data \
  --audit /var/lib/worldbisect-hub/audit \
  --recipient age1_REPLACE_WITH_THE_PUBLIC_RECIPIENT \
  --output /var/backups/worldbisect-hub/2026-09-20T120000Z.age
sudo systemctl start worldbisect-hub
```

Use a unique output name. Check the backup command's exit status before marking
the backup successful, then restart the service even if the backup failed and
verify liveness plus a synthetic write/read/delete. The maintenance interval
includes snapshot creation and encryption because both writer locks remain held.
Keep the script in an operator-controlled path readable by the service account.

The destination directory must already exist with mode 0700. Output files are
0600, atomically published without replacement and fsynced along with their
directory. Input symlinks, hard links, special files, group/world-readable input
files and symlinked path components are rejected. Temporary plaintext exists
only in a private staging directory and is removed after completion; deletion
is not secure erasure on SSDs or copy-on-write storage. Encrypt the underlying
volume when that residual exposure is unacceptable.

Defaults are 256 MiB of input, 20,000 files and 16 MiB per file. Global
`--max-bytes` and `--max-files` options can raise the total limits up to 2 GiB
and 100,100 files respectively; the individual file limit remains 16 MiB.
Encryption/decryption has a 180-second timeout and a bounded output size.
The wrapper refuses to overwrite an existing backup.

Copy the completed encrypted object to separately controlled backup storage.
Give those objects an explicit retention policy consistent with the sensitivity
of summaries and audit events. Hub retention does not delete external backups.

## Restore and verify before switching traffic

Restore into a **new** directory under an existing private parent, using the
same reviewed hub version initially. The target must not already exist, even
as an empty directory. Restoration authenticates the complete ciphertext,
checks archive paths/types, quotas and every manifest hash, fsyncs private files,
and publishes the entire directory atomically without replacement.

For configurations with CI publisher policies, restore intentionally adds or
extends `ci_quarantine_until` to at least eleven minutes after restoration.
The hub rejects OIDC publishing with 503 until that deadline and checks again
before accepting a submission. The interval exceeds the accepted CI token
lifetime and skew allowance, so rolling back the replay ledger cannot make an
already used, still-live CI token reusable. Existing longer quarantine deadlines
are preserved. Do not remove or shorten this field during recovery. Report data
and the audit chain stay byte-for-byte as archived; this documented config
change occurs only after the original manifest has been verified.

```bash
umask 077
mkdir -m 0700 /secure/recovery
python3 scripts/hub-backup.py restore \
  --backup /secure/backups/2026-09-20T120000Z.age \
  --identity /secure/offline/hub-backup-identity.txt \
  --target /secure/recovery/hub-restored
bin/worldbisect-hub serve \
  --config /secure/recovery/hub-restored/config.json \
  --data /secure/recovery/hub-restored/data \
  --audit-dir /secure/recovery/hub-restored/audit \
  --listen 127.0.0.1:8091
```

Before starting the recovery instance, verify the restored audit chain with
`worldbisect-hub audit-verify --audit-dir /secure/recovery/hub-restored/audit`.
If you retained a trusted checkpoint separately, add `--expected-head HASH` to
compare it with the restored head. Verification requires the server to be
stopped because it takes the same audit writer lock. Then compare expected report IDs/content, confirm that
read credentials cannot write, and run a synthetic create/read/delete. An old
backup may contain reports now beyond retention; starting the hub can purge
them. Preserve the original encrypted backup when investigating an incident.

Restoring an old configuration can revive revoked credentials and remove recent
access changes. Reconcile the recovered configuration with the approved current
credential inventory, revoke old keys and rotate credentials before exposing the
restored service. Never copy raw credentials into the backup manifest.

Stop the recovery instance before moving directories into service. Reassign
ownership to the service account if recovery ran as another user; retain 0700
directories and 0600 files. Keep the previous installation for rollback until
validation succeeds. Repeat a drill after schema/configuration changes and on
a schedule chosen by the operator.

## Failure handling

| Symptom | Operator action |
| --- | --- |
| Backup reports a live writer lock | Stop the hub and audit writer; do not delete or replace lock files. |
| Age cannot decrypt | Check the selected identity and backup object; corruption or a wrong key must fail without a published restore. |
| Archive/path/hash validation fails | Quarantine the object and recover another known-good backup. Do not bypass verification. |
| Audit verification fails | Preserve original evidence, block new traffic, investigate and recover a verified chain. |
| Disk/inode exhaustion | Stop growth, expand storage or remove reviewed expired backups; do not delete active audit segments manually. |
| Service cannot start after a restore | Check version, directory ownership/permissions, configuration and audit logs before switching traffic. |

## Reproducible development checks

```bash
python3 -m unittest discover -s scripts -p test_hub_backup.py -v
python3 scripts/hub-backup-e2e.py --binary bin/worldbisect-hub
```

Real encryption tests require both `age` and `age-keygen` on PATH; otherwise
the test output explicitly reports those tests as skipped. For a nonstandard
installation set `WORLDBISECT_TEST_AGE` and `WORLDBISECT_TEST_AGE_KEYGEN` to the
verified executables. Tests cover ciphertext tampering, wrong identities,
both live-writer locks, traversal/link rejection, file/byte limits, restored
permissions, manifest integrity and refusal to replace existing destinations.
They also verify the enforced restore quarantine and preservation of a longer
existing quarantine deadline for CI publishing.
The E2E drill creates ten synthetic reports, confirms a running server prevents
backup, stops the server, encrypts and restores config/data/audit, compares the
audit head, starts the restored hub and checks exact report equality plus
read-only access. It also verifies that the real restored server rejects CI
publishing with 503 and the remaining quarantine interval. It prints actual sizes and durations for that run, with no
tokens or submitted report bodies.

Local timing measurements describe only the explicitly reported drill's small
dataset on its test host. They establish neither a recovery-time objective nor
production throughput or availability.

An initial local Linux drill with age v1.3.2 and ten synthetic reports preserved
all report fields and the audit head, rejected a live-server backup, enforced the
CI restore quarantine and retained read-only authorization after restart. The 13-file snapshot held 13,738 plaintext
bytes and produced 30,920 encrypted bytes. The measured backup and restore steps
took 0.008 s and 0.011 s respectively; these exclude process startup, off-host
transfer, operator work and end-to-end service recovery. Re-run the drill for
your data size and infrastructure rather than treating these numbers as a target.
