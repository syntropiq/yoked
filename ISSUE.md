# Issue: Dynamic Context Sizing (NumCtx)

## Problem Description

The current Ollama implementation for managing model context size (`NumCtx`) exhibits several behaviors that can lead to inefficient resource utilization and unexpected performance characteristics:

1.  **Static Default:** `NumCtx` is initially set from an environment variable (`OLLAMA_CONTEXT_LENGTH`) or a fixed default, which may not be optimal for all model interactions.
2.  **Request Overrides:** While `NumCtx` can be overridden in API requests, its interaction with internal scaling mechanisms is unclear.
3.  **Scheduler Scaling:** The `server/sched.go` component *still* scales `NumCtx` by `numParallel` within its model fitting logic (`pickBestFullFitByLibrary`), despite previous attempts to remove this. This leads to excessive memory pre-allocation, even for short prompts, as observed by the user ("ram usage grows incredibly fast... even if the only thing I've said is hello"). This suggests that `NumCtx` is being interpreted as a total KV cache size for parallel operations rather than a per-request context window. This re-scaling is causing test failures in `server/sched_test.go`.
4.  **Hard Capping:** For embedding requests, `NumCtx` is explicitly capped at the GGUF model's `context_length`, which might not always represent the true maximum context the model can handle if `context_length` is a recommended value rather than an absolute limit.
5.  **Unclear `NumPredict` Impact:** The default behavior of `NumPredict = -1` currently means "use up to 10 times the current `NumCtx` for prediction" (`llm/server.go:770`). This can lead to very large potential response lengths when `NumCtx` is large, but the current plan's fixed default of 1024 for `maxResponseTokens` when `NumPredict` is -1 would override this, potentially limiting desired long responses.

The goal is to transition to a dynamic `NumCtx` sizing approach that optimizes resource usage by calculating `NumCtx` based on the actual incoming message length plus the expected response length, capped at the model's true maximum context.

## Proposed Solution Overview

The proposed solution involves intercepting and recalculating `NumCtx` at the API request handling layer (`server/routes.go`) before it reaches the scheduler and backend. This calculated `NumCtx` will be based on the sum of the incoming message's token length and the `max_response_tokens` (or a sensible default), rounded up to the nearest multiple of 1024, and strictly capped by the model's maximum context length. The scheduler's current `NumCtx * numParallel` scaling will be removed, as the new `NumCtx` will already represent the desired effective context for a single request.

This approach aims to:
*   Prevent excessive memory pre-allocation by sizing the context dynamically.
*   Ensure efficient use of the model's context window.
*   Provide a clear and predictable mechanism for context management.
*   Disregard any `num_ctx` provided in the incoming request, as the system will now determine the optimal size.

## Test Plan Overview

The implementation of dynamic `NumCtx` sizing necessitates a review and update of existing test cases to ensure correctness and prevent regressions. The primary focus will be on:

1.  **`server/routes_generate_test.go`**: Adding new test cases to verify the dynamic `NumCtx` calculation, including scenarios with varying prompt lengths, `NumPredict` values (especially `-1`), rounding to 1024, and capping at `modelMaxCtx`. The mock server will be enhanced to capture the `api.Options` passed to it for assertion.
2.  **`server/sched_test.go`**: Modifying existing tests or adding new ones to confirm that the `NumCtx` passed to `llm.NewLlamaServer` is no longer scaled by `numParallel`. The `mockLlm` will be updated to capture these parameters for verification.
3.  **`server/prompt_test.go`**: No direct changes are expected, as this file tests the truncation logic based on a given `NumCtx`, which will now be dynamically provided from upstream.
4.  **`api/client_test.go` and `api/types_test.go`**: No changes are expected, as these files do not directly interact with `NumCtx` or `NumPredict` in a way that would be affected by the dynamic sizing.

A separate investigation into the exact behavior and implications of `NumPredict = -1` will be conducted before the final implementation of the dynamic `NumCtx` feature.

---

# CRITICAL DESIGN INCONSISTENCY DISCOVERED

## Debug Investigation Results

During investigation of test failures (June 7, 2025), I discovered a **critical design inconsistency** that fundamentally undermines the dynamic NumCtx implementation:

### Root Cause: Handler Implementation Divergence

**The dynamic NumCtx calculation is ONLY implemented in `GenerateHandler` but NOT in `ChatHandler`**, creating inconsistent behavior across API endpoints that serve similar functions.

### Detailed Technical Analysis

#### 1. GenerateHandler Implementation (Lines 227-333 in server/routes.go)
```go
// Get model's maximum context length from GGUF metadata
kvData, _, err := getModelData(m.ModelPath, false)
modelMaxCtx := int(kvData.ContextLength())

// Temporarily schedule runner to get initial options and runner for tokenization
tempR, tempM, tempOpts, err := s.scheduleRunner(...)

// Calculate message length by tokenizing the prompt
tokens, err := tempR.Tokenize(c.Request.Context(), prompt)
messageLength = len(tokens)

// Determine max response tokens
maxResponseTokens := determineMaxResponseTokens(tempOpts.NumPredict, messageLength, modelMaxCtx)

// Calculate dynamic NumCtx
dynamicNumCtx := calculateDynamicNumCtx(messageLength, maxResponseTokens, modelMaxCtx)
req.Options["num_ctx"] = dynamicNumCtx

// Now schedule the runner again with the updated NumCtx
r, m, opts, err := s.scheduleRunner(c.Request.Context(), name.String(), caps, req.Options, req.KeepAlive)
```

#### 2. ChatHandler Implementation (Lines 1599+ in server/routes.go)
```go
// ChatHandler does NOT have any dynamic NumCtx calculation
// It directly calls scheduleRunner without tokenization or dynamic sizing:
r, m, opts, err := s.scheduleRunner(ctx, name.String(), caps, req.Options, req.KeepAlive)
```

### Test Failure Analysis

#### Failing Test: `TestDynamicNumCtxCalculation`
- **Expected**: Dynamic NumCtx calculation for ChatHandler
- **Actual**: ChatHandler bypasses all dynamic calculation
- **Result**: `mock.CapturedOptions.Runner.NumCtx = 0` (default/unmodified value)

#### Evidence from Test Output:
```
routes_generate_test.go:1140: expected NumCtx 2048, got 0
routes_generate_test.go:1145: expected numParallel > 0, got 0
```

#### Test Code Analysis:
```go
// Test calls ChatHandler but expects GenerateHandler behavior
w := createRequest(t, s.ChatHandler, req)  // Line 1132
// But ChatHandler doesn't have dynamic calculation!
```

### Diagnostic Logging Revealed:
- No dynamic calculation logs appear in test output
- Scheduler loads models with default NumCtx values
- Mock server captures unmodified options structure

### Impact Assessment

#### 1. User Experience Impact
- **Inconsistent Behavior**: `/api/generate` vs `/api/chat` endpoints behave differently
- **Resource Utilization**: ChatHandler uses static NumCtx, potentially wasting memory
- **Performance**: No dynamic optimization for chat interactions

#### 2. Development Impact
- **Test Failures**: Multiple test suites failing due to incorrect assumptions
- **Code Maintenance**: Duplicated logic makes future changes error-prone
- **Documentation Gap**: API behavior not clearly documented

#### 3. System Architecture Impact
- **Design Fragmentation**: Two similar endpoints with different NumCtx behavior
- **Logic Duplication**: Multiple code paths for similar functionality
- **Potential for Drift**: Future changes may only apply to one endpoint

### Questions for Architectural Review

#### 1. Design Intent Questions
- **Was this intentional?** Should ChatHandler have different NumCtx behavior?
- **Is there a technical reason** for the divergence between endpoints?
- **What was the original design intent** for NumCtx behavior across endpoints?

#### 2. Implementation Strategy Questions
- **Should behavior be unified?** Both endpoints serve LLM interaction purposes
- **Shared vs. Duplicated Logic?** Code reuse vs. endpoint-specific behavior
- **Backward Compatibility?** How to handle existing user expectations

#### 3. Testing Strategy Questions
- **How should tests be structured** for both endpoints?
- **What behavior should be tested** - unified or divergent?
- **Should there be separate test suites** for each endpoint?

### Evidence Files for Reference

#### Key Implementation Files:
- `server/routes.go` (GenerateHandler vs ChatHandler comparison)
- `server/routes_generate_test.go` (failing test cases)
- `api/types.go` (Options structure and conversion)

#### Added Diagnostic Logging:
- `calculateDynamicNumCtx()` function logging parameters and results
- NumCtx override logging in GenerateHandler
- Scheduler loading progress logging

### Recommended Next Steps

1. **Architectural Decision Required**: Determine intended system behavior
2. **Design Unification Assessment**: Evaluate shared vs. separate logic approaches
3. **Implementation Strategy**: Choose between:
   - Add dynamic calculation to ChatHandler
   - Update tests to match current implementation
   - Remove dynamic calculation entirely
4. **Testing Strategy Revision**: Align tests with final design decision

### Current System State

- **GenerateHandler**: ✅ Has dynamic NumCtx calculation
- **ChatHandler**: ❌ No dynamic NumCtx calculation  
- **Tests**: ❌ Expect ChatHandler to have dynamic calculation
- **Documentation**: ❌ Behavior not clearly specified

This inconsistency represents a fundamental design decision point that requires architectural review before proceeding with fixes.