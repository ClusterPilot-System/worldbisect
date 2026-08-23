# Environment-cause example

`check.sh` succeeds only when `WORLDBISECT_EXAMPLE_MODE=good`. Capture a good
and bad world with that variable changed, then compare them. The expected
`PROVEN` result means only that this supported environment difference repairs
and reproduces this oracle within the captured intervention model.

It does not prove that the environment is the only possible cause on the host,
and it does not turn surrounding logs or traces into causal proof. See
[`../../docs/proof-boundary.md`](../../docs/proof-boundary.md).
