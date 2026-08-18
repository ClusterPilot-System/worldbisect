# Operations guide

## Service account

Packages create the system user and group `worldbisect`. The daemon should not run as root. Grant only the filesystem access needed for explicitly authorized workspaces and commands.

## Initialization

```bash
sudo worldbisectd init \
  --config /etc/worldbisect/config.json \
  --data-dir /var/lib/worldbisect
```

Store the one-time token in an external secret manager. The raw token cannot be recovered from the configuration.

## Start

```bash
sudo systemctl enable --now worldbisectd
sudo systemctl status worldbisectd
```

## Health and metrics

```bash
curl http://127.0.0.1:8787/api/v1/health
curl -H "Authorization: Bearer $TOKEN" \
  http://127.0.0.1:8787/api/v1/metrics
```

Metrics are cumulative process-local counters. Logs are JSON on stderr or in journald. Spans are stored under the data directory.

## Backup

Back up:

- configuration;
- data directory;
- signing private key;
- public certificate artifacts needed for audit or incident response.

Stop the daemon or use a filesystem snapshot for a consistent backup.

## Restore

Restore the configuration and data directory with original ownership and restrictive permissions. Run:

```bash
worldbisect audit --store /var/lib/worldbisect
worldbisect doctor --store /var/lib/worldbisect
```

Verify existing causal certificates after restoration.

## Upgrades

1. Back up configuration and data.
2. Read `CHANGELOG.md`.
3. Install the package.
4. Start the daemon; migrations run under the store transaction lock.
5. Verify health, audit chain, and representative captures.

Unsupported future schema versions fail closed.

## Capacity

The dominant costs are workspace hashing, stored output, and repeated experiments. Set quotas before enabling remote execution. Keep the API loopback-only unless there is a documented remote-access requirement.

## Incident response

For suspected compromise:

1. stop the daemon;
2. revoke bearer tokens by removing their hashes from configuration;
3. preserve configuration, audit log, traces, and relevant job records;
4. rotate the causal signing key when confidentiality or integrity may be affected;
5. verify the audit chain and installed binary checksum;
6. report upstream privately when the issue affects WorldBisect.
