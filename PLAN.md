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
    *   The line `pending.opts.NumCtx = pending.origNumCtx * numParallel` (line 212 in `server/sched.go`) will be **removed**. The `numParallel` parameter passed to `llm.NewLlamaServer` will still be used for internal backend parallelism, but it will not scale the `NumCtx` value itself.
    *   The `pickBestFullFitByLibrary` and `pickBestPartialFitByLibrary` functions, and `llm.EstimateGPULayers`, will continue to use `numParallel` for their memory estimations, but the `NumCtx` they receive will be the *final calculated* `NumCtx` for a single request.

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