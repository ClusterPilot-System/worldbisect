# Anonymized user reports

This directory contains public case studies derived from real WorldBisect use
only after explicit consent, private source verification, and maintainer
redaction. A case study is not a support commitment or a claim that the same
cause is universal.

## Publication gate

Before publishing a case, a maintainer must record privately that:

1. the reporter authorized anonymized publication;
2. the source report was reviewed against the proposed summary;
3. names, organizations, infrastructure identifiers, secrets, private paths,
   command arguments, raw output, workspace content, and identifying timing
   were removed or generalized;
4. the stated result status and next action match the executable contract; and
5. the report does not imply support for an undocumented platform or factor.

The public case itself contains only the release version, generalized Linux
platform, sanitized symptom, result status, bounded factor class, evidence
boundary, and safe next action. Do not commit source captures or private
verification notes.

## Case template

Use [`TEMPLATE.md`](TEMPLATE.md) for a reviewed report. Replace every
placeholder and add the `consented` and `source-verified` markers only after
the publication gate is complete.

No real-world case study is published yet. The repository's demo fixtures are
synthetic and must never be presented as user evidence.
