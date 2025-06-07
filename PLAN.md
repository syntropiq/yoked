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
- `TestGenerate`
- `TestNumCtxNotScaledByNumParallel`

**Note:** The successful resolution of the CreateRequest issue demonstrates that some failures may be due to similar "simple and stupid" oversights rather than complex architectural problems.

## Investigation Strategy (Leveraging Debug Mode) - Revised June 7, 2025

The primary goal is to gather more precise information about the nature of the regressions and implement the agreed-upon fixes. This will involve creating targeted subtasks for the `debug` mode.

### ✅ Phase 1 COMPLETED: Address `TestGenerateChat` Failures (Critical Design Inconsistency)

This phase focused on unifying the `NumCtx` calculation for `ChatHandler` with `GenerateHandler` and updating the `calculateDynamicNumCtx` formula.

*   **Subtask 1.1: Unify `NumCtx` Handling for `ChatHandler`** ✅ COMPLETED
    *   **Goal:** Implement dynamic `NumCtx` calculation in `ChatHandler`.
    *   **Outcome:** `ChatHandler` already had dynamic `NumCtx` calculation implemented using the `calculateAndSetDynamicNumCtx` reusable function.

*   **Subtask 1.2: Refine Dynamic `NumCtx` Calculation Formula** ✅ COMPLETED
    *   **Goal:** Implement the new dynamic `NumCtx` formula in `calculateDynamicNumCtx`.
    *   **Outcome:** Updated `calculateDynamicNumCtx` function in [`server/routes.go`](server/routes.go) with the new formula: `calculatedNumCtx = max(4096, ((messageLength * 2 + 1023) / 1024) * 1024)`. This is capped at `modelMaxCtx`. Added comprehensive logging.

*   **Subtask 1.3: Update `TestGenerateChat` Expectations** ✅ COMPLETED
    *   **Goal:** Adjust `TestGenerateChat` to reflect the new dynamic `NumCtx` behavior.
    *   **Outcome:** Updated test expectations in `TestDynamicNumCtxCalculation`, `TestDynamicNumCtxGenerateHandler`, and `TestNumCtxNotScaledByNumParallel` to reflect the new formula.

### ✅ Phase 2 COMPLETED: Address `TestRequestsSameModelSameRequest` Failures

This phase focused on investigating and resolving model loading and concurrency issues within the scheduler.

*   **Subtask 2.1: Investigate Model Loading in Scheduler** ✅ COMPLETED
    *   **Goal:** Pinpoint why model loading is failing or if there's a race condition/mismanagement of model instances.
    *   **Outcome:** Identified `NumCtx` normalization inconsistency in `needsReload` function.

*   **Subtask 2.2: Analyze "Incompatible Model" Error** ✅ COMPLETED
    *   **Goal:** Understand the conditions under which the "this model may be incompatible with your version of Ollama" error is triggered.
    *   **Outcome:** Determined not to be the root cause of `TestRequestsSameModelSameRequest` failures.

*   **Subtask 2.3: Implement Fix for Model Loading/Compatibility** ✅ COMPLETED
    *   **Goal:** Resolve the underlying issue causing model loading failures.
    *   **Outcome:** Fixed `NumCtx` normalization in `server/sched.go`'s `needsReload` function (line ~650) by normalizing both `optsExisting.NumCtx` and `optsNew.NumCtx` by `runner.numParallel`.

### Phase 3: Documentation Updates

After each fix, I will update the following documentation files:

*   [`ISSUE.md`](ISSUE.md): Document the problem diagnosis, root cause, and resolution.
*   [`PLAN.md`](PLAN.md): Update the status of the resolved tests and refine the plan for any remaining items.
*   [`TODO.md`](TODO.md): Mark completed subtasks and update the list of remaining tasks.

### New Performance Diagnosis Plan: "Time to First Token" Degradation (June 7, 2025)

**Objective:** Add comprehensive logging to both client and server to diagnose "time to first token" degradation and dynamic context growth/truncation issues.

**Relationship with `llama.cpp`:** Ollama uses `llama.cpp` as a C/C++ library directly linked into the Go application via `cgo`. Performance issues are likely due to overhead of loading/re-initializing the model within `llama.cpp` or internal `llama.cpp` state management.

*   **Subtask 3.1: Client-side "Time to First Token" Stopwatch**
    *   **Goal:** Measure and display "time to first token" in client's `--verbose` output.
    *   **Instructions for Code Mode:**
        *   Identify relevant client-side verbose output location (e.g., `cmd` or `app` directories).
        *   Implement a stopwatch that starts when the request is sent and stops when the first token is received.
        *   Display this duration in the `--verbose` output.

*   **Subtask 3.2: Server-side Context Size and Truncation Logging**
    *   **Goal:** Log `num_ctx` per message and truncation warnings in server logs.
    *   **Instructions for Code Mode:**
        *   Enhance `calculateAndSetDynamicNumCtx` in [`server/routes.go`](server/routes.go) to log `dynamicNumCtx` after calculation, including request ID and model name.
        *   Identify prompt truncation logic (likely `server/prompt.go`).
        *   Add log entries *before* truncation (original message length, `NumCtx` limit) and *after* truncation (final truncated length, amount lopped off).

*   **Subtask 3.3: Verification of New Logging**
    *   **Goal:** Confirm new logging is accurate and provides expected diagnostic information.
    *   **Instructions for Debug Mode:**
        *   Run a user acceptance test scenario that reproduces "time to first token" degradation and involves context growth/truncation.
        *   Review client-side `--verbose` output and server logs.

*   **Subtask 3.4: Update Documentation (Final)**
    *   **Goal:** Document the findings and resolution of the "time to first token" issue.
    *   **Instructions for Architect Mode:**
        *   Update `ISSUE.md` with the diagnosis and resolution.
        *   Update `PLAN.md` with the completed subtasks.
        *   Update `TODO.md` to mark these new tasks as completed.

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

    G --> H[New Task: Diagnose Time to First Token Degradation]
    H --> H1[Subtask 3.1: Client-side Stopwatch]
    H1 --> H2[Subtask 3.2: Server-side Context Logging]
    H2 --> H3[Subtask 3.3: Verify New Logging]
    H3 --> H4[Subtask 3.4: Update Documentation (Final)]
```