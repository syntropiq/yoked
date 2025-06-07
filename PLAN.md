# Plan: Regression Investigation and Resolution

## Objective

Conduct a thorough investigation into the widespread test regressions, focusing on potential "simple and stupid" oversights related to `NumCtx` and `numParallel` handling, and the impact of the Spongebob truncation method. Based on the findings, formulate a revised, detailed plan for resolution.

## Current Status

The dynamic `NumCtx` calculation has been unified between `GenerateHandler` and `ChatHandler`, and `TestDynamicNumCtxCalculation` is passing. However, numerous other tests are now failing, indicating deeper architectural or implementation issues.

## Investigation Strategy (Leveraging Debug Mode)

Instead of immediately attempting fixes, the primary goal is to gather more precise information about the nature of the regressions. This will involve creating targeted subtasks for the `debug` mode.

### Phase 1: Core `NumCtx` and `numParallel` Interaction

*   **Hypothesis:** The `NumCtx` value might still be incorrectly scaled or passed, or `numParallel` might be unexpectedly `0` or `1` in certain contexts, leading to incorrect resource allocation or test expectations. The Spongebob truncation might also be interacting unexpectedly.
*   **Debug Subtasks:**
    *   **Subtask 1: `TestNumCtxNotScaledByNumParallel` Investigation**
        *   **Goal:** Determine why `TestNumCtxNotScaledByNumParallel` is failing.
        *   **Instructions for Debug Mode:**
            *   Add extensive logging within [`server/sched.go`](server/sched.go) (specifically `pickBestFullFitByLibrary` and `pickBestPartialFitByLibrary`) to log the values of `req.opts.NumCtx`, `req.origNumCtx`, `p` (numParallel), and the result of any scaling operations.
            *   Log the parameters passed to `llm.PredictServerFit` and `llm.EstimateGPULayers`.
            *   Run `TestNumCtxNotScaledByNumParallel` in isolation and capture the detailed log output.
            *   Report on the exact values of these variables at critical points and identify where the scaling (if any) is occurring or where `NumCtx` is being misinterpreted.
    *   **Subtask 2: `TestDynamicNumCtxGenerateHandler` Investigation**
        *   **Goal:** Understand why subtests within `TestDynamicNumCtxGenerateHandler` are failing, despite `TestDynamicNumCtxCalculation` passing.
        *   **Instructions for Debug Mode:**
            *   Add logging within [`server/routes.go`](server/routes.go) (specifically `calculateAndSetDynamicNumCtx` and its callers in `GenerateHandler`) to log `messageLength`, `maxResponseTokens`, `modelMaxCtx`, `calculatedNumCtx`, and `finalNumCtx`.
            *   Enhance the mock server in [`server/routes_generate_test.go`](server/routes_generate_test.go) to log the `api.Options` (especially `NumCtx`) it receives from `s.scheduleRunner`.
            *   Run `TestDynamicNumCtxGenerateHandler` with all subtests and capture detailed log output.
            *   Report on the calculated `NumCtx` values and the values received by the mock server, highlighting any discrepancies.

### Phase 2: Model Creation and Management Regressions

*   **Hypothesis:** Changes related to dynamic `NumCtx` or other recent modifications might have inadvertently affected how models are created, their metadata is processed, or how they are managed in the system.
*   **Debug Subtasks:**
    *   **Subtask 3: `TestCreate*` Tests Investigation**
        *   **Goal:** Pinpoint the exact cause of failures in `TestCreateFromBin`, `TestCreateFromModel`, `TestCreateRemovesLayers`, `TestCreateUnsetsSystem`, `TestCreateMergeParameters`, `TestCreateReplacesMessages`, `TestCreateTemplateSystem`, `TestCreateLicenses`, `TestCreateDetectTemplate`.
        *   **Instructions for Debug Mode:**
            *   For each failing `TestCreate*` test, add logging within [`server/create.go`](server/create.go) to trace the flow of model metadata (e.g., `context_length`, `template`, `system`, `parameters`, `licenses`, `layers`) as it's read from the GGUF, processed, and stored.
            *   Log the expected vs. actual values of these properties at various stages.
            *   Run these tests individually and capture detailed logs.
            *   Report on the specific discrepancies found for each test.
    *   **Subtask 4: `TestManifestCaseSensitivity` Investigation**
        *   **Goal:** Determine the root cause of the `TestManifestCaseSensitivity` failure.
        *   **Instructions for Debug Mode:**
            *   Add logging in the manifest parsing logic (likely in `model/` or `parser/`) to show how model names/paths are being compared and if case sensitivity is being correctly applied or ignored where expected.
            *   Run the test and report on the exact comparison logic and values.

### Phase 3: Chat Generation and Truncation

*   **Hypothesis:** The "Spongebob truncation" or the tokenization process for chat messages, especially with interleaved system messages, might be misbehaving with the new dynamic `NumCtx`.
*   **Debug Subtasks:**
    *   **Subtask 5: `TestGenerateChat (messages_with_interleaved_system)` Investigation**
        *   **Goal:** Understand why `TestGenerateChat (messages_with_interleaved_system)` is failing.
        *   **Instructions for Debug Mode:**
            *   Add logging within [`server/routes.go`](server/routes.go) (specifically in `ChatHandler` and the `calculateAndSetDynamicNumCtx` call) to log the raw `req.Messages`, the templated prompt, and the tokenized `messageLength`.
            *   Add logging within [`server/prompt.go`](server/prompt.go) (specifically `chatPrompt`) to log the `opts.NumCtx` received and the state of the prompt before and after truncation.
            *   Run the test and report on the exact prompt content, token counts, and truncation behavior.

### Phase 4: Scheduler Concurrency and Model Request Handling

*   **Hypothesis:** The scheduler's ability to handle concurrent requests for the same model might be affected by the dynamic `NumCtx` or other recent changes.
*   **Debug Subtasks:**
    *   **Subtask 6: `TestRequestsSameModelSameRequest` Investigation**
        *   **Goal:** Determine why `TestRequestsSameModelSameRequest` is failing.
        *   **Instructions for Debug Mode:**
            *   Add logging within [`server/sched.go`](server/sched.go) to trace how requests for the same model are queued, processed, and if model instances are being correctly reused or reloaded.
            *   Log the `NumCtx` and `numParallel` values associated with each request as it enters the scheduler.
            *   Run the test and report on the sequence of events and any unexpected behavior in request handling or resource allocation.

### Phase 5: General Model Lifecycle Tests

*   **Hypothesis:** `TestDelete`, `TestList`, and `TestShow` failures are likely downstream effects of issues in model creation or management.
*   **Debug Subtasks:**
    *   **Subtask 7: `TestDelete`, `TestList`, `TestShow` Investigation**
        *   **Goal:** Confirm if these failures are secondary to `TestCreate*` issues, or if they represent independent problems.
        *   **Instructions for Debug Mode:**
            *   Run these tests after the `TestCreate*` issues are investigated (but not necessarily fixed).
            *   If they still fail, add logging to their respective handlers in [`server/routes.go`](server/routes.go) and underlying functions to trace model lookup, deletion, and listing operations.
            *   Report on any specific errors or unexpected behavior.

## Revised Mermaid Diagram: Regression Investigation Flow

```mermaid
graph TD
    A[Start: Analyze Failing Tests & User Feedback] --> B{Identify Regression Categories}
    B -- NumCtx/numParallel --> C[Phase 1: Core NumCtx/numParallel Interaction]
    B -- Model Creation/Management --> D[Phase 2: Model Creation/Management Regressions]
    B -- Chat/Truncation --> E[Phase 3: Chat Generation & Truncation]
    B -- Scheduler Concurrency --> F[Phase 4: Scheduler Concurrency]
    B -- General Lifecycle --> G[Phase 5: General Model Lifecycle Tests]

    C --> C1[Debug Subtask 1: TestNumCtxNotScaledByNumParallel]
    C --> C2[Debug Subtask 2: TestDynamicNumCtxGenerateHandler]
    D --> D1[Debug Subtask 3: TestCreate* Tests]
    D --> D2[Debug Subtask 4: TestManifestCaseSensitivity]
    E --> E1[Debug Subtask 5: TestGenerateChat (interleaved_system)]
    F --> F1[Debug Subtask 6: TestRequestsSameModelSameRequest]
    G --> G1[Debug Subtask 7: TestDelete, TestList, TestShow]

    C1 & C2 & D1 & D2 & E1 & F1 & G1 --> H[Collect Debug Reports]
    H --> I[Architect: Synthesize Debug Findings]
    I --> J[Architect: Formulate Revised Resolution Plan]
    J --> K[Update TODO.md with new tasks]
    K --> L[Signal Completion (attempt_completion)]
```