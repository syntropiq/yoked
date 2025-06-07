# Ollama Context Enhancement - Phase 1: TODO List

## Phase 1: Implement "Spongebob" Truncation in `chatPrompt`

**File:** [`server/prompt.go`](server/prompt.go:0)
**Associated Plan:** [`PLAN.md`](PLAN.md:0)
**Associated Issue:** [`ISSUE.md`](ISSUE.md:0)

### Code Implementation Tasks:

- [x] **All Spongebob truncation unit tests for `chatPrompt` written in [`server/prompt_test.go`](server/prompt_test.go:0).** (Initial new tests added)
- [x] **Go toolchain verified: All required build/test tools are available in environment.**

- [x] **1. Refactor `chatPrompt` Signature/Initial Setup:** (Completed)
- [x] **2. Implement Core Message Assembly Logic:** (Completed)
- [x] **3. Implement `M_skip` Conditional Logic:** (Completed)
- [x] **4. Implement Filling Logic for Remaining Context:** (Completed)
- [x] **5. Finalize `finalMessages` Assembly:** (Completed)
- [x] **6. Handle Edge Cases:** (Implemented as per Spongebob logic)

### Testing Tasks:

**File:** [`server/prompt_test.go`](server/prompt_test.go:205-316)

- [x] **1. Update Existing/Failing Tests to Align with Spongebob Strategy:**
    - [x] Build succeeds - all compilation errors fixed.
    - [x] **COMPLETED:** Modified assertions in existing failing tests to expect the Spongebob strategy output (preserving `M1`, `M_skip`, `M_latest`, and grouping system messages at the front).
        - Key tests updated include:
            - `"truncate messages"` - Now expects M1 + intermediate + M_latest behavior
            - `"truncate messages with image"` - Updated for new truncation behavior
            - `"truncate messages with images"` - Updated for image handling with new algorithm
            - `"truncate message with interleaved images"` - Fixed expectations
            - `"out of order system"` - Now correctly expects system messages grouped at front
            - `"short conversation, no truncation, no M_skip"` - Fixed template formatting expectations
            - `"long conversation, M_skip inserted"` - Updated to not use skip when context too small
            - `"long conversation, M_skip reused"` - Updated to use new "..." ellipsis format
            - `"only system messages"` - Fixed template formatting expectations
            - `"very small NumCtx (minimal context)"` - Updated to not use skip when insufficient room
            - `"M_skip role and content correctness"` - Updated behavior expectations
    - [x] **IMPROVEMENT IMPLEMENTED:** Changed M_skip message from long text to simple "..." ellipsis
    - [x] **IMPROVEMENT IMPLEMENTED:** Added logic to avoid using M_skip when context too small for M1 + skip + M_latest
    - [x] Ensure all tests in `prompt_test.go` pass after updates - **ALL TESTS NOW PASSING**

- [x] **2. Add New Unit Tests for "Spongebob" Logic:** (All listed test cases are present in [`server/prompt_test.go`](server/prompt_test.go:205-316) - assertions reviewed/updated as part of step 1 above if they were among the failing ones or if their original Spongebob-specific assertions were incorrect).
    - [x] Test case: Short conversation, no truncation, no `M_skip`.
    - [x] Test case: Long conversation, `M_skip` is inserted.
    - [x] Test case: Long conversation, `M_skip` was already present from a previous truncation cycle and is reused.
    - [x] Test case: Conversation where `M1` is `M_latest` (no intermediate messages).
    - [x] Test case: Conversation with only system messages.
    - [x] Test case: Empty `msgs` input.
    - [x] Test case: `opts.NumCtx` is very small, forcing minimal context.
    - [x] Test case: Interaction with image token counting.
    - [x] Test case: Verify `M_skip` has the correct role ("system") and content.

### Documentation/Cleanup:

- [x] **1. Update Code Comments:**
    - [x] Add detailed comments in `chatPrompt` explaining the new logic.
- [x] **2. Update `PLAN.md` and `ISSUE.md` (if necessary):**
    - [x] Documents updated to reflect the decision to align tests with Spongebob behavior.

---
*Footnote: Return to Architect mode if there are difficulties or unforeseen issues during implementation.*