# Issue: Dynamic Context Sizing (NumCtx)

## Problem Description

The current Ollama implementation for managing model context size (`NumCtx`) exhibits several behaviors that can lead to inefficient resource utilization and unexpected performance characteristics:

1.  **Static Default:** `NumCtx` is initially set from an environment variable (`OLLAMA_CONTEXT_LENGTH`) or a fixed default, which may not be optimal for all model interactions.
2.  **Request Overrides:** While `NumCtx` can be overridden in API requests, its interaction with internal scaling mechanisms is unclear.
3.  **Scheduler Scaling:** The `server/sched.go` component *still* scales `NumCtx` by `numParallel` within its model fitting logic (`pickBestFullFitByLibrary`), despite previous attempts to remove this. This leads to excessive memory pre-allocation, even for short prompts, as observed by the user ("ram usage grows incredibly fast... even if the only thing I've said is hello"). This suggests that `NumCtx` is being interpreted as a total KV cache size for parallel operations rather than a per-request context window. This re-scaling is causing test failures in `server/sched_test.go`.
4.  **Hard Capping:** For embedding requests, `NumCtx` is explicitly capped at the GGUF model's `context_length`, which might not always represent the true maximum context the model can handle if `context_length` is a recommended value rather than an absolute limit.
5.  **Unclear `NumPredict` Impact:** The default behavior of `NumPredict = -1` currently means "use up to 10 times the current `NumCtx` for prediction" (`llm/server.go:770`). This can lead to very large potential response lengths when `NumCtx` is large, but the current plan's fixed default of 1024 for `maxResponseTokens` when `NumPredict` is -1 would override this, potentially limiting desired long responses.

The goal is to transition to a dynamic `NumCtx` sizing approach that optimizes resource usage by calculating `NumCtx` based on the actual incoming message length plus the expected response length, capped at the model's true maximum context.

---

## New Performance Observation: "Time to First Token" Degradation (June 7, 2025)

During user acceptance testing, a significant degradation in "time to first token" has been observed, worsening with each subsequent pass (especially when spitting out ~4k more tokens each time). This occurs despite RAM usage remaining relatively stable, leading to uncertainty about whether context truncation is occurring.

**Symptoms:**
- Increasing latency for the first token on successive requests.
- Stable RAM usage, suggesting context might not be growing as expected or is being truncated.
- Lack of detailed server-side logging regarding per-message context size and truncation events.

**Hypothesized Causes (to be investigated):**
1.  **Model Reloads/Re-initialization:** Despite previous fixes for `needsReload`, the `llama.cpp` model might still be undergoing unexpected reloads or re-initializations within the Go process, leading to startup overhead on each pass.
2.  **Internal `llama.cpp` State Management:** Even if the model remains loaded, `llama.cpp` itself might have internal state management or caching mechanisms that are not performing optimally with growing context, leading to increased processing time for the first token.
3.  **Silent Truncation:** The context might be silently truncated without explicit server-side logging, leading to unexpected behavior and performance characteristics.

**Relationship with `llama.cpp`:**
Ollama uses `llama.cpp` as a C/C++ library directly linked into the Go application via `cgo`. This means performance issues are likely due to:
- Overhead of loading/re-initializing the model within `llama.cpp`.
- Initial prompt evaluation within `llama.cpp`.
- Go-side processing before/after `llama.cpp` calls.

## Proposed Solution Overview

The proposed solution involves intercepting and recalculating `NumCtx` at the API request handling layer (`server/routes.go`) before it reaches the scheduler and backend. This calculated `NumCtx` will be based on the sum of the incoming message's token length and the `max_response_tokens` (or a sensible default), rounded up to the nearest multiple of 1024, and strictly capped by the model's maximum context length. The scheduler's current `NumCtx * numParallel` scaling will be removed, as the new `NumCtx` will already represent the desired effective context for a single request.

This approach aims to:
*   Prevent excessive memory pre-allocation by sizing the context dynamically.
*   Ensure efficient use of the model's context window.
*   Provide a clear and predictable mechanism for context management.
*   Disregard any `num_ctx` provided in the incoming request, as the system will now determine the optimal size.

## Test Plan Overview

The implementation of dynamic `NumCtx` sizing necessitates a review and update of existing test cases to ensure correctness and prevent regressions. The primary focus will be on:

1.  **`server/routes_generate_test.go`**: Adding new test cases to verify the dynamic `NumCtx` calculation, including scenarios with varying prompt lengths, `NumPredict` values (especially `-1`), and model maximum context lengths. This will ensure that the dynamic sizing logic behaves as expected across different conditions.
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

### Recommended Next Steps (Revised June 7, 2025)

1.  **Architectural Decision: Unify `NumCtx` Handling for `ChatHandler`**:
    *   **Decision:** Unify behavior; `ChatHandler` will also implement dynamic `NumCtx` calculation.
    *   **Action:** Refactor dynamic `NumCtx` calculation logic from `GenerateHandler` into a reusable function in `server/routes.go` and integrate it into `ChatHandler`.
    *   **Action:** Update `TestGenerateChat` expectations to match the new dynamic `NumCtx` behavior.

2.  **Refine Dynamic `NumCtx` Calculation Formula:**
    *   **Proposed New Formula for `calculateDynamicNumCtx`:**
        1.  Calculate `baseNumCtx = messageLength * 2`.
        2.  Round `baseNumCtx` up to the nearest multiple of `1024` (serving as the `n_ctx_per_seq` barrier).
        3.  Apply a floor: `calculatedNumCtx = max(roundedNumCtx, 4096)`.
        4.  Apply a cap: `calculatedNumCtx = min(calculatedNumCtx, modelMaxCtx)`.
        5.  Return `calculatedNumCtx`.
    *   **Action:** Update `calculateDynamicNumCtx` in `server/routes.go` with this new formula.
    *   **Action:** Re-evaluate `determineMaxResponseTokens` to ensure alignment.

3.  **Address `TestRequestsSameModelSameRequest` Failures:**
    *   **Action:** Add logging in `server/sched.go` to trace model queuing, processing, reuse, and reloading.
    *   **Action:** Investigate the "incompatible model" error source in `llm/server.go` or related files.
    *   **Action:** Implement and verify fixes for model loading/compatibility.

### Current System State (Updated June 7, 2025)

-   **GenerateHandler**: ✅ Has dynamic NumCtx calculation
-   **ChatHandler**: ❌ No dynamic NumCtx calculation (Target for unification)
-   **Tests**:
    *   `TestDynamicNumCtxCalculation` ✅ FIXED
    *   `TestDynamicNumCtxGenerateHandler` ✅ FIXED
    *   `TestGenerateChat` ❌ FAILING (Target for unification and new formula)
    *   `TestRequestsSameModelSameRequest` ❌ FAILING (Target for investigation)
-   **Documentation**: ❌ Behavior not clearly specified (Will be updated as part of this plan)

This revised plan addresses the critical design inconsistency and refines the dynamic `NumCtx` calculation for more predictable and efficient resource management.

---

## Current Test Failures (June 7, 2025) - RESOLVED

### ✅ RESOLVED: API Field Deprecation Issue

**Root Cause Identified:** An automated tool "fixed deprecated" fields by changing `Name` to `Model` in `api.CreateRequest` structures throughout the test files. However, the server's `CreateHandler` still uses `r.Name` (the deprecated field) for model name validation, while the new `Model` field was not being processed.

**Technical Details:**
- `api.CreateRequest` has both `Model string` (new) and `Name string` (deprecated)
- Server's `CreateHandler` validates using `name := model.ParseName(r.Name)` (line 58 in create.go)
- The automated "fix" changed test CreateRequest instances to use `Model:` instead of `Name:`
- This caused "invalid model name" errors because `r.Name` was empty while `r.Model` contained the intended value

**Tests Affected:**
- `TestDynamicNumCtxCalculation` ✅ FIXED
- `TestDynamicNumCtxGenerateHandler` ✅ FIXED
- All other CreateRequest tests ✅ FIXED

**Resolution Applied:**
- Reverted all test CreateRequest instances back to using the deprecated `Name` field
- Changed `Model: "test-name"` back to `Name: "test-name"` in:
  - `server/routes_generate_test.go` (10 instances fixed)
- Tests now pass correctly with proper model name validation

**Files Modified:**
- `server/routes_generate_test.go`: Reverted Model → Name in CreateRequest structs

**Verification:**
```
$ go test -v -run TestDynamicNumCtx github.com/ollama/ollama/server
=== RUN   TestDynamicNumCtxCalculation
--- PASS: TestDynamicNumCtxCalculation (0.04s)
=== RUN   TestDynamicNumCtxGenerateHandler
--- PASS: TestDynamicNumCtxGenerateHandler (0.03s)
PASS
ok  	github.com/ollama/ollama/server	0.555s
```

**Note:** This highlights the need for better coordination between API field deprecation and server implementation updates. The server should ideally handle both `Model` and `Name` fields during the transition period.

### Remaining Investigation Items (Updated June 7, 2025)

The following tests may still need investigation if they continue to fail:

- `TestGenerate`
- `TestNumCtxNotScaledByNumParallel`

**Important Note on Test vs. Implementation Discrepancies:**
If a test failure is due to an expectation difference (e.g., the test expects 1024 but the implementation correctly produces 8192), the test should be updated to reflect the correct implementation behavior. If, however, the implementation is returning 0, nil, or some other "failed to work" mode, then the underlying implementation needs to be investigated and fixed. This distinction is crucial for efficient debugging and resolution.

---

## ✅ RESOLVED: `TestRequestsSameModelSameRequest` Failures (June 7, 2025)

**Root Cause Identified and Fixed:**
- **Problem**: `NumCtx` normalization inconsistency in the `needsReload` function at line ~637 in `server/sched.go`.
- **Technical Issue**: The function normalized `optsExisting.NumCtx` by dividing by `runner.numParallel` but left `optsNew.NumCtx` unchanged, causing `reflect.DeepEqual` comparison to fail and trigger unnecessary model reloads.
- **Evidence**: Logs showed `optsExisting.NumCtx=2048` vs `optsNew.NumCtx=4096` after normalization.

**Fix Implemented:**
- **Location**: `server/sched.go`, `needsReload` function (line ~650).
- **Change**: Added `optsNew.NumCtx = optsNew.NumCtx / runner.numParallel` to normalize both options equally.
- **Result**: Both `NumCtx` values now properly normalized (e.g., 2048 vs 2048), allowing runner reuse.

**Verification Results:**
- ✅ `TestRequestsSameModelSameRequest` now passes consistently.
- ✅ `TestRequestsSimpleReloadSameModel` continues to pass (no regression).
- ✅ `TestNeedsReload` continues to pass (no regression).
- ✅ Test execution time improved from timeout (500ms) to immediate success (<1ms).

**Files Modified:**
- `server/sched.go`: Fixed `NumCtx` normalization bug in `needsReload` function.

**Key Insight:** This fix ensures that when multiple requests come in for the same model with identical configurations, the scheduler correctly reuses the existing model runner instead of unnecessarily triggering a reload, which was causing the test timeout failures.

---

# Issue: Dynamic KV Cache Sizing and Per-Request Model Unloading

## Problem Description

The current KV cache management does not precisely align with the dynamically calculated `num_ctx` for each request, leading to potential context loss or inefficient resource utilization. While `num_ctx` is dynamically determined per request, the underlying KV cache of the loaded model instance has a fixed size (typically from Modelfile defaults). This mismatch can result in:

1.  **Context Loss:** If the dynamically calculated `num_ctx` for a request exceeds the loaded model's fixed KV cache size, context may be truncated prematurely, even if the model itself supports a larger context. The `ShiftCacheSlot` mechanism, while improved, is a fallback for an already full cache, not a primary context management tool.
2.  **Suboptimal Resource Use:** Models might be loaded with a fixed KV cache size that is either too small (leading to truncation) or unnecessarily large (wasting VRAM) for a given request's actual needs.
3.  **Inconsistent Behavior:** The system's ability to maintain context is dependent on the fixed KV cache size of the loaded model, not the dynamically calculated `num_ctx` that reflects the current request's requirements.

## Critical Requirement

The system must **never lose context until the model's maximum context is reached**, but it should **not start the model off with max settings** if a smaller context is sufficient for the current request. This necessitates a precise matching of the loaded model's KV cache size to the request's dynamic `num_ctx`.

## Chosen Approach: Per-Request Model Reload with Specific Dynamic `NumCtx`

To meet the critical requirement, the chosen approach is to **reload the model for each request, setting its KV cache size to that request's specific dynamically calculated `num_ctx`**.

**Rationale:**
While this approach incurs a performance penalty (increased "time to first token" due to frequent model loading/unloading), it guarantees absolute context accuracy, which is paramount for use cases like legal fact-checking where context loss can have severe repercussions.

**How it addresses the problem:**
*   **Precise KV Cache Sizing:** Each loaded model instance will have its KV cache exactly sized to the `dynamicNumCtx` required by the current request.
*   **Eliminates Premature Truncation:** Context will only be lost if the `dynamicNumCtx` itself exceeds the model's absolute maximum context, not due to a mismatch with a fixed, smaller KV cache.
*   **Resource Optimization (per-request):** While loading overhead is high, the VRAM usage for a given inference will be precisely what's needed, not an over-provisioned maximum.

## Proposed Solution Overview

The solution involves modifying the scheduler to explicitly unload existing model instances and load new ones with a KV cache size that matches the `dynamicNumCtx` calculated for each incoming request.

### Key Changes:

1.  **Propagate Dynamic `NumCtx`:** The `dynamicNumCtx` calculated in `server/routes.go` will be passed to the scheduler via the `LlmRequest` struct.
2.  **Scheduler Logic Update:**
    *   The scheduler will compare the request's `DynamicNumCtx` with the `NumCtx` of any currently loaded model instance.
    *   If they don't match, the existing instance will be unloaded, and a new instance will be loaded with its KV cache explicitly set to the `request_dynamic_num_ctx`.
    *   After each inference, the model instance will be explicitly unloaded (unless an identical request is immediately pending).
3.  **Runner Initialization:** Ensure the `llm.NewLlamaServer` and subsequent runner initialization correctly use the provided `NumCtx` to set the KV cache size.

This approach ensures that the model's KV cache is always "ample enough" for the intended inference, as it will be precisely sized to the dynamically calculated context length, thereby minimizing context loss.

---

# Issue: Incomplete Logging for "Time to First Token" Degradation Diagnosis

## Problem Description

During the verification of the "Time to First Token" (TTFT) degradation diagnosis system (Subtask 3.3 in `PLAN.md`), two critical deficiencies in the current logging implementation were identified:

1.  **Incomplete Truncation Logging**: The system logs when a context size check occurs before truncation, but it lacks detailed logging *after* truncation, specifically showing the final truncated length and the exact amount of context that was removed. This information is crucial for understanding how context growth/truncation impacts TTFT degradation.
2.  **Missing Request ID Population**: Server-side logs attempt to use a `requestID` from the context for correlation, but this `requestID` is not being populated anywhere in the request lifecycle. This makes it difficult to correlate client-side TTFT measurements with specific server-side events like dynamic `NumCtx` calculations or truncation events.

These issues hinder effective diagnosis and make it challenging to fully assess the impact of dynamic KV cache sizing and per-request model unloading on TTFT.

## Proposed Solution Overview: Enhance Logging for TTFT Degradation Diagnosis

The proposed solution involves implementing comprehensive logging for context truncation and ensuring proper request ID correlation across client and server logs.

### Key Changes:

1.  **Enhance Server-Side Truncation Logging**:
    *   Add detailed log entries in `server/prompt.go` after any context truncation occurs.
    *   These logs will include the final token count, original token count, and the number of tokens "lopped off" (removed).
    *   The `requestID` will be included for correlation.

2.  **Implement Request ID Population**:
    *   Generate a unique `requestID` for each incoming API request in `server/routes.go`.
    *   Propagate this `requestID` through the request's `context.Context` using `context.WithValue`.
    *   Ensure all relevant server-side logs (e.g., dynamic `NumCtx` calculation, truncation events) include this `requestID`.

This approach will provide the necessary diagnostic information to accurately assess TTFT degradation and the behavior of dynamic context management.