# Recovery parameter field-name contract

The original fixed head f4ef1b5d's Go CI failed in the ordinary Test job:
[run40471481296/job122079294549](https://github.com/particleflow/Memoh/actions/runs/40471481296/job/122079294549).
The only failed test/package in that log was `internal/apperror.TestRequestFieldNamesAreLiterals`:

```
internal/handlers/session_user_input_recovery.go:72:33:
apperror.FieldInvalid names its field with name; write the field as a string literal
```

The new recovery handler iterated a map and passed its key variable to FieldInvalid. The repository's existing global AST contract requires literals at each request-field naming call. The source handler's own directed tests and lint did not run that cross-package guard; a successful local source aggregate was not full-repository success.

The repair spells out the same bot_id/session_id/decision_id UUID checks and the same existing `request.field_invalid` errors using literal field names. Valid request behavior, authorization, admission, recovery identity, unknown handling and API/SDK/schema are unchanged. No test, rule, workflow, timeout or exception was weakened. Three added malformed-path scenarios verify the exact existing code/field and zero recovery service calls, alongside the original eleven cases.

The same global test reproduced the original failure locally before editing: [before.txt](before.txt), exit1. After the repair, that guard and all fourteen recovery HTTP scenarios passed: [after.txt](after.txt), exit0.

```
go test -p=2 ./internal/apperror ./internal/handlers \
  -run '^(TestRequestFieldNamesAreLiterals|TestAcceptedUserInputRecoveryHTTPUsesAccountAndOwnedSession)$' \
  -count=1
```

This Go-only repair does not change Web/Harmony product or generated API files relative to f4ef1b5d. Reuse the existing client build; do not rebuild/sign HAP for this parameter-style correction. New server source must pass its own ordinary CI before publication. The failed original run remains evidence and was not rerun. No production/Windows layout changes, data migration or model call were performed.

The normal staged aggregate passed without skips: handler-package golangci-lint2.10.1 (142.62s), full handler tests (4.743s test time; 7.48s command), total150.34s. See [staged-checks.txt](staged-checks.txt).
