# Plan: Regression Investigation and Resolution

## Objective

Conduct a thorough investigation into the widespread test regressions, focusing on potential "simple and stupid" oversights related to `NumCtx` and `numParallel` handling, and the impact of the Spongebob truncation method. Based on the findings, formulate a revised, detailed plan for resolution.

## Current Status (Updated June 7, 2025)

### ✅ RESOLVED: API Field Deprecation Issue

**Successfully resolved `TestDynamicNumCtxCalculation` and `TestDynamicNumCtxGenerateHandler`:**
- **Root Cause:** Automated tool changed `Name` to `Model` in CreateRequest structs, but server still uses deprecated `Name` field
- **Resolution:** Reverted test CreateRequest instances back to using `Name` field
- **Files Fixed:** `server/routes_generate_test.go` (10 instances)
- **Status:** ✅ Tests now passing

### Remaining Tests to Investigate:
- `TestGenerateChat`
- `TestGenerate`
- `TestNumCtxNotScaledByNumParallel`
- `TestRequestsSameModelSameRequest`

**Note:** The successful resolution of the CreateRequest issue demonstrates that some failures may be due to similar "simple and stupid" oversights rather than complex architectural problems.

## Investigation Strategy (Leveraging Debug Mode) - Revised June 7, 2025

The primary goal is to gather more precise information about the nature of the regressions and implement the agreed-upon fixes. This will involve creating targeted subtasks for the `debug` mode.

### Phase 1: Address `TestGenerateChat` Failures (Critical Design Inconsistency)

This phase focuses on unifying the `NumCtx` calculation for `ChatHandler` with `GenerateHandler` and updating the `calculateDynamicNumCtx` formula.

*   **Subtask 1.1: Unify `NumCtx` Handling for `ChatHandler`**
    *   **Goal:** Implement dynamic `NumCtx` calculation in `ChatHandler`.
    *   **Instructions for Code Mode:**
        *   Refactor the dynamic `NumCtx` calculation logic from `GenerateHandler` into a reusable function (e.g., `calculateAndSetDynamicNumCtx`) in [`server/routes.go`](server/routes.go).
        *   Integrate this reusable function into `ChatHandler` in [`server/routes.go`](server/routes.go) to ensure it also performs dynamic `NumCtx` calculation.
    *   **Instructions for Debug Mode:**
        *   Add logging within [`server/routes.go`](server/routes.go) (specifically in `ChatHandler` and the `calculateAndSetDynamicNumCtx` call) to log the raw `req.Messages`, the templated prompt, and the tokenized `messageLength`.
        *   Run `TestGenerateChat` and capture detailed log output to verify the dynamic calculation.

*   **Subtask 1.2: Refine Dynamic `NumCtx` Calculation Formula**
    *   **Goal:** Implement the new dynamic `NumCtx` formula in `calculateDynamicNumCtx`.
    *   **Instructions for Code Mode:**
        *   Modify the `calculateDynamicNumCtx` function in [`server/routes.go`](server/routes.go) to implement the following logic:
            1.  Calculate `baseNumCtx = messageLength * 2`.
            2.  Round `baseNumCtx` up to the nearest multiple of `1024` (this will serve as the `n_ctx_per_seq` barrier). Formula: `((baseNumCtx + 1023) / 1024) * 1024`.
            3.  Apply a floor: `calculatedNumCtx = max(roundedNumCtx, 4096)`.
            4.  Apply a cap: `calculatedNumCtx = min(calculatedNumCtx, modelMaxCtx)`.
            5.  Return `calculatedNumCtx`.
        *   Re-evaluate `determineMaxResponseTokens` in [`server/routes.go`](server/routes.go) to ensure it aligns with the new `calculateDynamicNumCtx` logic.
    *   **Instructions for Debug Mode:**
        *   Add logging within `calculateDynamicNumCtx` to log `messageLength`, `maxResponseTokens`, `modelMaxCtx`, `baseNumCtx`, `roundedNumCtx`, `finalNumCtx` (after floor and cap).
        *   Run `TestGenerateChat` and `TestDynamicNumCtxCalculation` (if needed) to verify the new calculation.

*   **Subtask 1.3: Update `TestGenerateChat` Expectations**
    *   **Goal:** Adjust `TestGenerateChat` to reflect the new dynamic `NumCtx` behavior.
    *   **Instructions for Code Mode:**
        *   Modify assertions in `TestGenerateChat` in [`server/routes_generate_test.go`](server/routes_generate_test.go) to expect the `NumCtx` values calculated by the new dynamic formula.
    *   **Instructions for Debug Mode:**
        *   Run `TestGenerateChat` to confirm it passes with the updated expectations.

### Phase 2: Address `TestRequestsSameModelSameRequest` Failures

This phase focuses on investigating and resolving model loading and concurrency issues within the scheduler.

*   **Subtask 2.1: Investigate Model Loading in Scheduler**
    *   **Goal:** Pinpoint why model loading is failing or if there's a race condition/mismanagement of model instances.
    *   **Instructions for Debug Mode:**
        *   Add extensive logging within [`server/sched.go`](server/sched.go) to trace how requests for the same model are queued, processed, and if model instances are being correctly reused or reloaded.
        *   Specifically, log the model name and path being loaded, and any errors during `NewLlamaServer` calls.
        *   Run `TestRequestsSameModelSameRequest` and capture detailed log output.

*   **Subtask 2.2: Analyze "Incompatible Model" Error**
    *   **Goal:** Understand the conditions under which the "this model may be incompatible with your version of Ollama" error is triggered.
    *   **Instructions for Debug Mode:**
        *   Investigate the source of this error in [`llm/server.go`](llm/server.go) or related model loading/compatibility checks.
        *   Analyze the log output from Subtask 2.1 for clues.

*   **Subtask 2.3: Implement Fix for Model Loading/Compatibility**
    *   **Goal:** Resolve the underlying issue causing model loading failures.
    *   **Instructions for Code Mode:**
        *   Implement necessary code changes in `server/sched.go` or `llm/server.go` based on the investigation from Subtasks 2.1 and 2.2.
    *   **Instructions for Debug Mode:**
        *   Run `TestRequestsSameModelSameRequest` to verify the fix.

### Phase 3: Documentation Updates

After each fix, I will update the following documentation files:

*   [`ISSUE.md`](ISSUE.md): Document the problem diagnosis, root cause, and resolution.
*   [`PLAN.md`](PLAN.md): Update the status of the resolved tests and refine the plan for any remaining items.
*   [`TODO.md`](TODO.md): Mark completed subtasks and update the list of remaining tasks.

### Revised Mermaid Diagram: Regression Investigation and Resolution Flow

```mermaid
graph TD
    A[Start: Analyze Remaining Failing Tests] --> B{TestGenerateChat Failing?}
    B -- Yes --> C[Phase 1: Fix TestGenerateChat]
    C --> C1[Subtask 1.1: Unify NumCtx Handling for ChatHandler]
    C1 --> C2[Subtask 1.2: Refine Dynamic NumCtx Calculation Formula]
    C2 --> C3[Subtask 1.3: Update TestGenerateChat Expectations]

    A --> D{TestRequestsSameModelSameRequest Failing?}
    D -- Yes --> E[Phase 2: Fix TestRequestsSameModelSameRequest]
    E --> E1[Subtask 2.1: Investigate Model Loading in Scheduler]
    E1 --> E2[Subtask 2.2: Analyze "Incompatible Model" Error]
    E2 --> E3[Subtask 2.3: Implement Fix for Model Loading/Compatibility]

    C3 & E3 --> F[Phase 3: Documentation Updates]
    F --> G[Signal Completion to Orchestrator]
```