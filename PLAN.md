# Plan: Regression Investigation and Resolution

## Objective

Conduct a thorough investigation into the widespread test regressions, focusing on potential "simple and stupid" oversights related to `NumCtx` and `numParallel` handling, and the impact of the Spongebob truncation method. Based on the findings, formulate a revised, detailed plan for resolution.

## Current Status

The following tests are currently failing:
- `TestGenerateChat`
- `TestGenerate`
- `TestDynamicNumCtxCalculation`
- `TestDynamicNumCtxGenerateHandler`
- `TestNumCtxNotScaledByNumParallel`
- `TestRequestsSameModelSameRequest`

## Investigation Strategy (Leveraging Debug Mode)

The primary goal is to gather more precise information about the nature of the regressions. This will involve creating targeted subtasks for the `debug` mode.

### Phase 1: Core `NumCtx` and `numParallel` Interaction

*   **Subtask 1: `TestNumCtxNotScaledByNumParallel` Investigation**
    *   **Goal:** Determine why `TestNumCtxNotScaledByNumParallel` is failing.
    *   **Instructions for Debug Mode:**
        *   Add extensive logging within [`server/sched.go`](server/sched.go) (specifically `pickBestFullFitByLibrary` and `pickBestPartialFitByLibrary`) to log the values of `req.opts.NumCtx`, `req.origNumCtx`, `p` (numParallel), and the result of any scaling operations.
        *   Log the parameters passed to `llm.PredictServerFit` and `llm.EstimateGPULayers`.
        *   Run `TestNumCtxNotScaledByNumParallel` in isolation and capture the detailed log output.
        *   Report on the exact values of these variables at critical points and identify where the scaling (if any) is occurring or where `NumCtx` is being misinterpreted.

*   **Subtask 2: `TestDynamicNumCtxGenerateHandler` Investigation**
    *   **Goal:** Understand why subtests within `TestDynamicNumCtxGenerateHandler` are failing. This investigation should also provide insights into `TestDynamicNumCtxCalculation` failures.
    *   **Instructions for Debug Mode:**
        *   Add logging within [`server/routes.go`](server/routes.go) (specifically `calculateAndSetDynamicNumCtx` and its callers in `GenerateHandler`) to log `messageLength`, `maxResponseTokens`, `modelMaxCtx`, `calculatedNumCtx`, and `finalNumCtx`.
        *   Enhance the mock server in [`server/routes_generate_test.go`](server/routes_generate_test.go) to log the `api.Options` (especially `NumCtx`) it receives from `s.scheduleRunner`.
        *   Run `TestDynamicNumCtxGenerateHandler` with all subtests and capture detailed log output.
        *   Report on the calculated `NumCtx` values and the values received by the mock server, highlighting any discrepancies.

### Phase 2: Chat Generation and Truncation

*   **Subtask 3: `TestGenerateChat` Investigation**
    *   **Goal:** Understand why `TestGenerateChat` is failing.
    *   **Instructions for Debug Mode:**
        *   Add logging within [`server/routes.go`](server/routes.go) (specifically in `ChatHandler` and the `calculateAndSetDynamicNumCtx` call) to log the raw `req.Messages`, the templated prompt, and the tokenized `messageLength`.
        *   Add logging within [`server/prompt.go`](server/prompt.go) (specifically `chatPrompt`) to log the `opts.NumCtx` received and the state of the prompt before and after truncation.
        *   Run the test and report on the exact prompt content, token counts, and truncation behavior.

### Phase 3: Scheduler Concurrency and Model Request Handling

*   **Subtask 4: `TestRequestsSameModelSameRequest` Investigation**
    *   **Goal:** Determine why `TestRequestsSameModelSameRequest` is failing.
    *   **Instructions for Debug Mode:**
        *   Add logging within [`server/sched.go`](server/sched.go) to trace how requests for the same model are queued, processed, and if model instances are being correctly reused or reloaded.
        *   Log the `NumCtx` and `numParallel` values associated with each request as it enters the scheduler.
        *   Run the test and report on the sequence of events and any unexpected behavior in request handling or resource allocation.

### Phase 4: General Generation Tests

*   **Subtask 5: `TestGenerate` Investigation**
    *   **Goal:** Understand why `TestGenerate` is failing.
    *   **Instructions for Debug Mode:**
        *   Add logging within [`server/routes.go`](server/routes.go) (specifically in `GenerateHandler` and the `calculateAndSetDynamicNumCtx` call) to log the raw request, the templated prompt, and the tokenized `messageLength`.
        *   Add logging within `llm/server.go` (where `NumPredict = -1` is handled) to see how `maxResponseTokens` is determined.
        *   Run the test and report on the exact prompt content, token counts, and any issues with `NumPredict` handling or response generation.

## Revised Mermaid Diagram: Regression Investigation Flow

```mermaid
graph TD
    A[Start: Analyze Failing Tests & User Feedback] --> B{Identify Regression Categories}
    B -- NumCtx/numParallel --> C[Phase 1: Core NumCtx/numParallel Interaction]
    B -- Chat/Truncation --> E[Phase 2: Chat Generation & Truncation]
    B -- Scheduler Concurrency --> F[Phase 3: Scheduler Concurrency]
    B -- General Generation --> G[Phase 4: General Generation Tests]

    C --> C1[Debug Subtask 1: TestNumCtxNotScaledByNumParallel]
    C --> C2[Debug Subtask 2: TestDynamicNumCtxGenerateHandler]
    E --> E1[Debug Subtask 3: TestGenerateChat]
    F --> F1[Debug Subtask 4: TestRequestsSameModelSameRequest]
    G --> G1[Debug Subtask 5: TestGenerate]

    C1 & C2 & E1 & F1 & G1 --> H[Collect Debug Reports]
    H --> I[Architect: Synthesize Debug Findings]
    I --> J[Architect: Formulate Revised Resolution Plan]
    J --> K[Update TODO.md with new tasks]
    K --> L[Signal Completion (attempt_completion)]
```