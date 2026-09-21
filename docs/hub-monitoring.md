# Monitor the hub's report lifecycle

`GET /healthz` proves that the hub process can answer a request. The optional
[operational probe](../scripts/hub-probe.py) checks an authenticated report's
create, read and delete path, including the audit writes those operations require.
It runs against the existing API and adds no public administration endpoint.

This is tooling for the source-built, self-hosted preview. It is not an operated
monitoring service, an availability commitment, a load test, or an independent
verification of a diagnosis. A successful probe does not test CI OIDC publishing,
retention sweeps, backup recovery, every workspace, or every proxy route.

## Provision a separate synthetic workspace

Use a dedicated `synthetic` workspace and a service identity with the `editor`
role. The monitor needs `reports:read`, `reports:write` and `reports:delete`.
Never use a real team's credential or share the CI publisher credential.

```bash
bin/worldbisect-hub credential --config hub.json \
  --subject operations-probe --kind service --workspace synthetic \
  --role editor --expires-in 168h
```

The command prints the new token once; capture it directly into the operator's
secret manager. Reload the serving hub with SIGHUP as described in the
[access guide](hub-access.md). Deliver only the raw token, optionally followed
by a newline, to a private regular file owned by the monitor account. Its mode
must be `0600` or stricter, with no group/other access; keep its parent private
and operator-controlled. Do not place the token in arguments, URLs, logs or
shell history. The monitor cannot use the hub configuration's token hash.

Run the probe from the reviewed source checkout using Python 3.10+ on Linux:

```bash
python3 scripts/hub-probe.py \
  --hub http://127.0.0.1:8090 \
  --workspace synthetic \
  --token-file /etc/worldbisect-probe/token \
  --timeout 20
```

For an actual deployment, use the same final trusted HTTPS origin and reverse
proxy path that clients use. HTTP is accepted only for a literal loopback IP.
The URL must be an origin with no credentials, path, query or fragment. Redirects
and ambient HTTP proxy settings are refused. TLS verification remains enabled.

Before writing, the probe verifies both the expected workspace and all three
effective scopes returned by `/api/v1/session`. A mismatch fails without a POST.
The report uses the fixed repository label `worldbisect/synthetic`, check name
`operations-probe`, `UNPROVEN` status, zero experiments and a fresh per-run
identifier. Its text explicitly describes an operational check, not a diagnosis.
No source files, commands, real repository metadata or customer content are sent.

The probe makes at most one POST, verifies the created report and reads it back,
then deletes its own report and verifies a subsequent GET returns `404`. It never
lists a workspace to guess which reports to delete. The timeout is a total
budget, including cleanup, and responses are bounded. Reserve capacity for these
requests when setting proxy limits; a quota or rate limit is a failed check,
not permission to bypass the hub's limits.

## Read the result and alert on missing results

The command emits one JSON object and exits `0` only when the full lifecycle
succeeds. Any failed phase exits `1`. The result includes:

| Field | Meaning |
| --- | --- |
| `schema_version` | Result contract version, currently `1`. |
| `ok` | All required phases completed successfully. |
| `checked_at` | UTC completion time for freshness checks; requires an accurate host clock. |
| `elapsed_seconds` | Monotonic elapsed duration, not a promised service latency. |
| `phases` | States for `session`, `create`, `read`, `delete` and `verify_deleted`. |
| `error` | A bounded phase/error code and, when available, HTTP status; no server body. |
| `cleanup` | Whether cleanup was attempted and its observed outcome. |
| `residue_possible` | A synthetic report may remain; investigate before repeated runs. |

Results omit credentials, destinations, workspace names, report IDs and report
content. Audit events still identify the configured service subject and
workspace under the hub's existing retention policy.

Send results to an external monitor. A pilot starting policy is to alert after
two consecutive failed probes, and separately when no completed result has
arrived for three minutes. These are example thresholds to tune, not an SLO.
Evaluate freshness from your monitoring system's receipt time as well as
`checked_at`: a stopped timer, failed host, missing Python executable, forced
termination or broken log forwarding may produce **no JSON at all**. An old
successful result must never remain indefinitely green.

Alert separately on expired credentials, disk/inode exhaustion, retention errors,
old backups and failed restore drills. A synthetic success covers one low-volume
workspace at one point in time; it cannot establish production capacity or uptime.

## Optional systemd schedule

The [service](../deploy/hub/worldbisect-hub-probe.service) and
[timer](../deploy/hub/worldbisect-hub-probe.timer) are installable templates.
Use a separate account with no access to the hub configuration, report or audit
directories. Install the reviewed script and deliver the token through your
secret manager before starting the timer:

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin worldbisect-monitor
sudo install -d -m 0755 /usr/local/lib/worldbisect
sudo install -m 0644 scripts/hub-probe.py /usr/local/lib/worldbisect/hub-probe.py
sudo install -d -m 0700 -o worldbisect-monitor -g worldbisect-monitor /etc/worldbisect-probe
# Deliver the raw token as /etc/worldbisect-probe/token, owned by
# worldbisect-monitor, mode 0600. No token is passed on the command line.
sudo install -m 0644 deploy/hub/worldbisect-hub-probe.service /etc/systemd/system/
sudo install -m 0644 deploy/hub/worldbisect-hub-probe.timer /etc/systemd/system/
sudo systemd-analyze verify /etc/systemd/system/worldbisect-hub-probe.service /etc/systemd/system/worldbisect-hub-probe.timer
sudo systemctl daemon-reload
sudo systemctl start worldbisect-hub-probe.service
sudo journalctl -u worldbisect-hub-probe.service -n 10 --no-pager
sudo systemctl enable --now worldbisect-hub-probe.timer
```

Review the origin, paths and resource/sandbox limits for your host first. The
timer waits 60 seconds after the previous invocation becomes inactive, with up
to five seconds of jitter. It does not queue missed checks while the host is off.
Keep `RemainAfterExit` disabled so later timer activations run again. The service
has a 30-second outer timeout around the default 20-second probe budget. A single
systemd unit prevents overlapping timer executions; avoid parallel manual runs.
See the upstream [timer](https://github.com/systemd/systemd/blob/main/man/systemd.timer.xml)
and [service](https://github.com/systemd/systemd/blob/main/man/systemd.service.xml)
documentation for scheduling and timeout semantics.

The templates do not install a monitoring receiver or deliver alerts. Check
`systemctl list-timers worldbisect-hub-probe.timer`, collect the service's JSON
journal messages, and wire both failure and freshness alerts before treating it
as monitored. A local syntax check does not validate every distribution's
systemd sandbox or a production deployment.

## Failure handling and credential rotation

| Observation | Next action |
| --- | --- |
| Session rejected or workspace/scopes mismatch | Check the expected workspace, membership, expiry and reload. Do not broaden the role in a real workspace. |
| HTTP `409` or `429` | Check synthetic report residue/quota or request frequency. Do not blindly repeat writes. |
| Liveness succeeds but the lifecycle fails | Check private storage, audit availability, free space/inodes and the failing phase. |
| Read-back differs or the response is invalid | Stop automated retries; check the configured origin and server version. |
| Cleanup fails or `residue_possible` is true | Pause the timer, inspect only the dedicated synthetic workspace and reconcile with its audit trail. |
| No fresh result | Check timer/service state, host reachability and log forwarding; do not assume the last success still applies. |

A network failure can occur after a report was stored but before its ID was
received. The probe never retries that POST or deletes an unrelated report.
If its own report is known, it attempts bounded cleanup; a process crash or
exhausted deadline can still leave residue. Normal hub retention eventually
removes old synthetic reports when storage and sweeps are healthy. Retention is
not immediate cleanup, and repeated failed checks can exhaust the workspace
quota; pause the timer and investigate instead of relying on endless retries.

Rotate before the configured expiry: provision a new credential for the same
subject/workspace, reload the hub, atomically replace the private token file,
run one successful check, then revoke the old key and reload again. Do not
disable expiry to conceal a failing monitor. Stop and disable the timer before
decommissioning its credential and workspace.

## Development verification

```bash
python3 -m unittest discover -s scripts -p test_hub_probe.py -v
make test-hub-probe
```

The real-binary drill checks a complete lifecycle and its audit events, denied
viewer/wrong-workspace/expired access, and storage/audit failures while liveness
continues to respond. It uses isolated temporary data and does not exercise a
hosted deployment, external alerts or CI OIDC publishing. Remaining launch work
stays tracked in [issue #63](https://github.com/ClusterPilot-System/worldbisect/issues/63).
