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
```mermaid
graph TD
    A[API Request (Generate/Chat)] --> B{Calculate DynamicNumCtx}
    B --> C[Create LlmRequest]
    C -- Add DynamicNumCtx --> D[Queue LlmRequest to Scheduler]

    D --> E[Scheduler Loop]
    E --> F{Find/Select RunnerRef}
    F -- Check if runner.LoadedNumCtx == request.DynamicNumCtx --> G{Match Found?}

    G -- No --> H[Unload Existing Runner (if any)]
    H --> I[Load New Runner Instance]
    I -- Pass request.DynamicNumCtx as kvSize --> J[Runner Initializes KV Cache to DynamicNumCtx]
    J --> K[Use Runner for Inference]

    G -- Yes --> K[Use Existing Runner for Inference]

    K --> L[Inference Complete]
    L --> M[Explicitly Unload Runner Instance]
    M --> N[Return Response]
```

# Plan: Dynamic KV Cache Sizing and Per-Request Model Unloading

## Objective

Implement a system where the KV cache size of the loaded model instance precisely matches the dynamically calculated `num_ctx` for each request. This ensures no context is lost until the model's maximum context is reached, while avoiding unnecessary loading of models with maximum settings. This approach prioritizes context accuracy over "time to first token" performance, acknowledging the trade-off for critical use cases.

## Phases and Subtasks

### Phase 1: Propagate Dynamic `NumCtx` to Scheduler Request

*   **Objective:** Modify the `LlmRequest` struct to carry the `dynamicNumCtx` from the API handlers to the scheduler.
*   **Subtasks:**
    *   **Subtask 1.1: Add `DynamicNumCtx` to `LlmRequest`**
        *   **Goal:** Extend the `LlmRequest` struct to include a field for the dynamically calculated context size.
        *   **File:** [`server/sched.go`](server/sched.go)
        *   **Action:** Add `DynamicNumCtx int` to the `LlmRequest` struct.
        *   **Mode:** Code
    *   **Subtask 1.2: Populate `DynamicNumCtx` in API Handlers**
        *   **Goal:** Ensure the `dynamicNumCtx` calculated in `routes.go` is correctly assigned to the `LlmRequest` before it's sent to the scheduler.
        *   **File:** [`server/routes.go`](server/routes.go) (specifically `GenerateHandler` and `ChatHandler`)
        *   **Action:** After `calculateAndSetDynamicNumCtx` determines `dynamicNumCtx`, set `llmReq.DynamicNumCtx = dynamicNumCtx`.
        *   **Mode:** Code

### Phase 2: Scheduler Logic for Dynamic KV Cache Sizing and Unloading

*   **Objective:** Modify the scheduler to load/unload model instances based on the `DynamicNumCtx` of the incoming request.
*   **Subtasks:**
    *   **Subtask 2.1: Update `runnerRef` to Store Loaded `NumCtx`**
        *   **Goal:** Enable the scheduler to know the KV cache size of a loaded runner instance.
        *   **File:** [`server/sched.go`](server/sched.go)
        *   **Action:** Add a field (e.g., `LoadedNumCtx int`) to the `runnerRef` struct, populated when the runner is successfully loaded.
        *   **Mode:** Code
    *   **Subtask 2.2: Modify Model Loading to Use `DynamicNumCtx`**
        *   **Goal:** Ensure new model instances are loaded with a KV cache size matching the request's `DynamicNumCtx`.
        *   **File:** [`server/sched.go`](server/sched.go) (specifically the `load` function or where `s.newServerFn` is called)
        *   **Action:** When calling `s.newServerFn` (`llm.NewLlamaServer`), set the `NumCtx` field within the `api.Options` argument (`req.opts`) to `req.DynamicNumCtx`.
        *   **Mode:** Code
    *   **Subtask 2.3: Update `needsReload` for Dynamic `NumCtx` Matching**
        *   **Goal:** Force a model reload if the loaded instance's KV cache size does not match the incoming request's `DynamicNumCtx`.
        *   **File:** [`server/sched.go`](server/sched.go)
        *   **Action:** Modify `runnerRef.needsReload` to compare `runner.LoadedNumCtx` with `pending.DynamicNumCtx`. If they differ, return `true` to trigger a reload.
        *   **Mode:** Code
    *   **Subtask 2.4: Implement Per-Request Model Unloading**
        *   **Goal:** Explicitly unload the model instance after each request is completed to free up resources.
        *   **File:** [`server/sched.go`](server/sched.go) (e.g., in `finishedProcessing` or a new dedicated function)
        *   **Action:** Add logic to call `runner.llama.Close()` (or equivalent unload method) on the `runnerRef` after a request is processed. Consider a very short grace period if an identical request is immediately pending to avoid redundant reloads.
        *   **Mode:** Code

### Phase 3: Verification and Performance Impact Assessment

*   **Objective:** Confirm correct behavior and quantify performance implications.
*   **Subtasks:**
    *   **Subtask 3.1: Verify KV Cache Sizing**
        *   **Goal:** Confirm that loaded model instances have the correct KV cache size.
        *   **Mode:** Debug
        *   **Instructions:** Add logging in `ollamarunner.NewInputCache` to print the `kvSize` it receives. Run requests with varying `dynamicNumCtx` and verify logs.
    *   **Subtask 3.2: Verify Model Unloading**
        *   **Goal:** Confirm models are unloaded as expected after inference.
        *   **Mode:** Debug
        *   **Instructions:** Add logging for model load/unload events in `server/sched.go` and `ollamarunner/runner.go`. Observe logs during sequential requests.
    *   **Subtask 3.3: Assess "Time to First Token" Penalty**
        *   **Goal:** Quantify the performance impact of frequent loading/unloading.
        *   **Mode:** Debug
        *   **Instructions:** Use the existing client-side "Time to First Token" stopwatch (Subtask 3.1 from previous plan) to measure latency for various request patterns.

### Phase 4: Documentation Updates

*   **Objective:** Update project documentation to reflect the new dynamic KV cache sizing and unloading strategy.
*   **Subtasks:**
    *   **Subtask 4.1: Update `ISSUE.md`**
        *   **Goal:** Document the problem, chosen approach, and high-level solution.
        *   **Mode:** Architect
    *   **Subtask 4.2: Update `PLAN.md`**
        *   **Goal:** Detail the implementation steps, subtasks, and current status.
        *   **Mode:** Architect
    *   **Subtask 4.3: Update `TODO.md`**
        *   **Goal:** Mark completed subtasks and list remaining tasks for implementation.
        *   **Mode:** Architect

## Dependencies and Considerations

*   **Existing `calculateAndSetDynamicNumCtx`:** This function in `server/routes.go` is crucial for determining the `dynamicNumCtx` and is a prerequisite.
*   **`numKeep` Setting:** The previous change to set `numKeep = s.cache.numCtx` in `runner/ollamarunner/runner.go` is consistent with this plan, as it effectively disables `ShiftCacheSlot`'s truncation when the KV cache is precisely sized.
*   **Performance Monitoring:** Close monitoring of "time to first token" and overall throughput will be essential during and after implementation.
*   **Error Handling:** Ensure robust error handling for model loading/unloading failures.
*   **Concurrency:** Pay close attention to locking mechanisms in the scheduler to prevent race conditions during model management.