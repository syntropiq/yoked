# Plan: Dynamic `NumCtx` Sizing Implementation

## Objective

Implement dynamic `NumCtx` sizing based on incoming message length + `max_response_tokens` (or default), rounded up to the nearest multiple of 1024, capped at the model's maximum context length. This will disregard any `num_ctx` value provided in the incoming request.

## Assumptions

*   The `context_length` in the GGUF metadata (`kvData.ContextLength()`) is the absolute maximum context length the model can support.
*   `NumPredict` (max response tokens) defaults to -1. If `NumPredict` is -1 or not set, it will be calculated as `modelMaxCtx - messageLength`, ensuring a minimum of 1024 tokens.

## Proposed Changes and Implementation Steps

1.  **Identify the Calculation Point:** The dynamic `NumCtx` will be calculated in `server/routes.go` within the `GenerateHandler` and `EmbedHandler` functions, *before* calling `s.scheduleRunner`.

2.  **Retrieve Model Max Context Length:**
    *   In `server/routes.go`, `kvData.ContextLength()` will be retrieved for both `GenerateHandler` and `EmbedHandler`. This will be our `modelMaxCtx`.

3.  **Determine `maxResponseTokens`:**
    *   For `GenerateHandler`:
        *   If `req.Options.NumPredict` is explicitly set and positive, that value will be used.
        *   If `req.Options.NumPredict` is -1 or not set:
            *   Calculate `remainingContext = modelMaxCtx - messageLength`.
            *   `maxResponseTokens = max(remainingContext, 1024)`. This ensures at least 1024 tokens for response, or more if there's ample remaining context.
        *   `maxResponseTokens` will be capped at `modelMaxCtx`.

4.  **Calculate `messageLength`:**
    *   For `GenerateHandler`: The `req.Prompt` and any `req.Messages` (after template application if `req.Raw` is false) will be tokenized to get the `messageLength`. This will involve calling `r.Tokenize`.
    *   For `EmbedHandler`: Each input string in `req.Input` will be tokenized to get its length. The `NumCtx` will be calculated per input string.

5.  **Implement Dynamic `NumCtx` Calculation:**
    *   The formula will be: `calculatedNumCtx = messageLength + maxResponseTokens`.
    *   `calculatedNumCtx` will be rounded up to the nearest multiple of 1024 using `((calculatedNumCtx + 1023) / 1024) * 1024`.
    *   `calculatedNumCtx` will be capped at `modelMaxCtx`: `finalNumCtx = min(calculatedNumCtx, modelMaxCtx)`.

6.  **Override `req.Options.NumCtx`:**
    *   `req.Options.Runner.NumCtx` will be set to `finalNumCtx` before calling `s.scheduleRunner`.

7.  **Adjust Scheduler Logic (`server/sched.go`):**
    *   **Remove `NumCtx` scaling in `pickBestFullFitByLibrary`**: The line `req.opts.NumCtx = req.origNumCtx * p` (around line 783) must be **removed**. This scaling is incorrect as `NumCtx` should represent the per-request context, not a scaled value for parallelism.
    *   **Adjust `llm.PredictServerFit` call**: Instead of modifying `req.opts.NumCtx` within `pickBestFullFitByLibrary`, `llm.PredictServerFit` (and subsequently `llm.EstimateGPULayers`) should be called with `req.origNumCtx` (the unscaled, dynamically calculated `NumCtx` from `routes.go`) and `p` (the `numParallel` value) as separate parameters. This ensures that memory estimation correctly accounts for both the per-request context and the parallelism factor without incorrectly scaling `NumCtx`.
    *   The `pickBestPartialFitByLibrary` function will also need to be reviewed to ensure similar scaling is not occurring there.

8.  **Review `server/prompt.go` (Spongebob Truncation):**
    *   The `chatPrompt` function already uses `opts.NumCtx` for its truncation logic. By overriding `opts.NumCtx` earlier in `server/routes.go`, the Spongebob truncation will automatically adapt to the new dynamic context size. No direct changes are expected here.

9.  **Review `llm/server.go`:**
    *   The `NewLlamaServer` function already takes `opts.NumCtx` and `numParallel` as separate parameters. The `--ctx-size` argument will receive the dynamically calculated `NumCtx`. The `EstimateGPULayers` function will use this `NumCtx` and `numParallel` for its calculations. No direct changes are expected here, but it's crucial to ensure `EstimateGPULayers` correctly interprets `NumCtx` as the per-request context and `numParallel` as the internal parallelism factor.

## Mermaid Diagram: Dynamic `NumCtx` Flow

```mermaid
graph TD
    A[Incoming API Request] --> B{server/routes.go: GenerateHandler/EmbedHandler}
    B -- Get modelMaxCtx from GGUF --> C[kvData.ContextLength()]
    B -- Determine messageLength (tokenize prompt/input) --> D[messageLength]
    B -- Determine maxResponseTokens (req.Options.NumPredict or default 1024) --> E[maxResponseTokens]
    F{Calculate calculatedNumCtx = messageLength + maxResponseTokens} --> G[Round up to nearest 1024]
    G --> H[Cap at modelMaxCtx: finalNumCtx = min(calculatedNumCtx, modelMaxCtx)]
    H --> I[Override req.Options.Runner.NumCtx = finalNumCtx]
    I --> J[s.scheduleRunner()]
    J -- Passes opts (with finalNumCtx) --> K[server/sched.go: GetRunner()]
    K -- Stores origNumCtx (now finalNumCtx) --> L[LlmRequest]
    L --> M[server/sched.go: processPending()]
    M -- REMOVE NumCtx * numParallel scaling --> N[Pass finalNumCtx to llm.NewLlamaServer]
    N -- Passes numParallel separately --> O[llm.NewLlamaServer (Backend)]
    O -- Passes --ctx-size (finalNumCtx) --> P[GGML Backend]
    P -- Uses finalNumCtx for KV Cache, Graph Size, RoPE --> Q[Model Execution]
    M -- (Spongebob Truncation uses finalNumCtx) --> R[server/prompt.go: chatPrompt()]
```

## CRITICAL DESIGN INCONSISTENCY RESOLUTION PLAN

**Goal:** Unify `NumCtx` calculation across `GenerateHandler` and `ChatHandler` in `server/routes.go` and update relevant tests.

**Detailed Plan:**

1.  **Refactor Dynamic `NumCtx` Calculation into a Reusable Function:**
    *   Extract the `NumCtx` calculation logic from `GenerateHandler` into a new, private helper function (e.g., `calculateAndSetDynamicNumCtx`) within [`server/routes.go`](server/routes.go). This function will take the necessary parameters (e.g., `modelMaxCtx`, `prompt`, `req.Options`, `s.scheduleRunner` related parameters for tokenization) and return the updated `api.Options` or directly modify `req.Options`.
    *   This promotes code reuse and reduces duplication.

2.  **Integrate Reusable Function into `ChatHandler`:**
    *   Call the newly created helper function within `ChatHandler` in [`server/routes.go`](server/routes.go) to perform the dynamic `NumCtx` calculation before calling `s.scheduleRunner`.
    *   Ensure that `ChatHandler` correctly handles message tokenization for chat messages (which might involve iterating through `req.Messages` and applying the chat template if `req.Raw` is false).

3.  **Update `server/routes_generate_test.go`:**
    *   Review and modify existing tests that call `ChatHandler` but expect `GenerateHandler` behavior. These tests should now pass as `ChatHandler` will have the dynamic `NumCtx` calculation.
    *   Add new test cases specifically for `ChatHandler` to verify its dynamic `NumCtx` calculation, including scenarios with varying message lengths, `NumPredict` values, rounding, and capping.
    *   Ensure the mock server captures the `api.Options` passed to it for assertion in `ChatHandler` tests.

**Mermaid Diagram: Unified Dynamic `NumCtx` Flow**

```mermaid
graph TD
    A[Incoming API Request] --> B{server/routes.go}
    B -- /api/generate --> C[GenerateHandler]
    B -- /api/chat --> D[ChatHandler]

    C --> E[Call calculateAndSetDynamicNumCtx()]
    D --> E

    E -- Get modelMaxCtx from GGUF --> F[kvData.ContextLength()]
    E -- Determine messageLength (tokenize prompt/input/messages) --> G[messageLength]
    E -- Determine maxResponseTokens (req.Options.NumPredict or default 1024) --> H[maxResponseTokens]
    I{Calculate calculatedNumCtx = messageLength + maxResponseTokens} --> J[Round up to nearest 1024]
    J --> K[Cap at modelMaxCtx: finalNumCtx = min(calculatedNumCtx, modelMaxCtx)]
    K --> L[Override req.Options.Runner.NumCtx = finalNumCtx]

    L --> M[s.scheduleRunner()]
    M -- Passes opts (with finalNumCtx) --> N[server/sched.go: GetRunner()]
    N -- Stores origNumCtx (now finalNumCtx) --> O[LlmRequest]
    O --> P[server/sched.go: processPending()]
    P -- REMOVE NumCtx * numParallel scaling --> Q[Pass finalNumCtx to llm.NewLlamaServer]
    Q -- Passes numParallel separately --> R[llm.NewLlamaServer (Backend)]
    R -- Passes --ctx-size (finalNumCtx) --> S[GGML Backend]
    S -- Uses finalNumCtx for KV Cache, Graph Size, RoPE --> T[Model Execution]
    P -- (Spongebob Truncation uses finalNumCtx) --> U[server/prompt.go: chatPrompt()]
```

## Test Plan

**Objective:** Identify all test files affected by the dynamic `NumCtx` sizing changes in `server/routes.go` and `server/sched.go`, analyze changed expectations, and formulate a plan for updating these tests.

**Key Areas of Impact:**

1.  **API Request Handling:** Tests that send `GenerateRequest` or `EmbedRequest` and explicitly set `NumCtx` or rely on default `NumCtx` behavior.
2.  **Scheduler Logic:** Tests that verify how runners are scheduled, loaded, and how `NumCtx` influences resource allocation or parallelism.
3.  **Prompt Truncation:** Tests that validate the "Spongebob truncation" logic, especially if they rely on a fixed context window.

**Step-by-Step Test Updates:**

1.  **`server/routes_generate_test.go`:**
    *   **Modify `newMockServer`:** Enhance to capture the `api.Options` passed to it, allowing inspection of the `NumCtx` value sent to the backend.
    *   **Add New Test Cases:**
        *   Verify dynamic `NumCtx` calculation with varying prompt lengths and `NumPredict` values (including `-1`).
        *   Assert that the `mockRunner.CompletionRequest.Options.NumCtx` matches the expected dynamic calculation (message length + `maxResponseTokens`, rounded up to nearest 1024, capped at `modelMaxCtx`).
        *   Confirm that `NumCtx` provided in the `api.ChatRequest` or `api.GenerateRequest` is disregarded.
        *   Add a specific test case for `NumPredict = -1` to ensure `maxResponseTokens` defaults to 1024 (or remaining context, capped at `modelMaxCtx`).

2.  **`server/sched_test.go`:**
    *   **Modify `mockLlm` (or `newServerFn` mock):** Capture `api.Options` and `numParallel` arguments passed to `llm.NewLlamaServer`.
    *   **Update Assertions:** In relevant tests (e.g., `TestLoad`, `TestRequestsSameModelSameRequest`), assert that the `NumCtx` received by the mock server is the *dynamically calculated* `NumCtx` and *not* scaled by `numParallel`. This confirms the removal of the `NumCtx * numParallel` scaling.
    *   **Review Memory Estimation Tests:** Ensure existing tests for `EstimateGPULayers` still pass. Consider adding new cases with dynamically calculated `NumCtx` and different `numParallel` values to confirm correct memory accounting.

3.  **`server/prompt_test.go`:**
    *   No direct changes are expected. The existing tests should continue to pass as `chatPrompt` uses the `opts.NumCtx` provided to it, which will now be the dynamically calculated value.

4.  **`api/client_test.go` and `api/types_test.go`:**
    *   No changes are expected, as these files do not directly interact with `NumCtx` or `NumPredict` in a way that would be affected by the dynamic sizing.