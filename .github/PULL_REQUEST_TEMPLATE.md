## Summary

<!-- 1–3 bullets describing what changed and why. -->

## Test plan

- [ ] `make lint` passes
- [ ] `make test` passes
- [ ] If this PR touches `pkg/extract/prompts.go`, `pkg/extract/preclassify.go`,
      `pkg/extract/normalize.go`, or any classifier/normaliser logic,
      `make test-regression` was run against the configured backend and
      the per-component accuracy lines are pasted below
      (see IMPL-0019 Phase 6).

<!-- If the test-regression checkbox is checked, paste the relevant
     accuracy output from `make test-regression` here. -->

## Notes

<!-- Optional: rollout/migration considerations, follow-ups, links to design docs. -->
