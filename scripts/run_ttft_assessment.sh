#!/bin/bash

# Comprehensive Time to First Token Assessment Runner
# This script runs the performance assessment and analyzes the results

set -e

MODEL="${1:-llama3.2:1b}"
RESULTS_DIR="ttft_assessment_$(date +%Y%m%d_%H%M%S)"

echo "=============================================="
echo "Time to First Token Performance Assessment"
echo "=============================================="
echo "Model: $MODEL"
echo "Results Directory: $RESULTS_DIR"
echo

# Check if ollama is running
if ! pgrep -x "ollama" > /dev/null; then
    echo "Starting Ollama server..."
    ollama serve &
    OLLAMA_PID=$!
    echo "Waiting for Ollama to start..."
    sleep 5
    CLEANUP_OLLAMA=true
else
    echo "Ollama server is already running"
    CLEANUP_OLLAMA=false
fi

# Function to cleanup
cleanup() {
    if [ "$CLEANUP_OLLAMA" = "true" ] && [ -n "$OLLAMA_PID" ]; then
        echo "Stopping Ollama server..."
        kill $OLLAMA_PID 2>/dev/null || true
    fi
}

trap cleanup EXIT

# Create results directory
mkdir -p "$RESULTS_DIR"

# Run the assessment
echo "Running TTFT assessment..."
./scripts/assess_ttft_penalty.sh "$MODEL" | tee "$RESULTS_DIR/assessment.log"

# Check if we have results
if [ ! -d "ttft_assessment_results" ]; then
    echo "ERROR: No results generated"
    exit 1
fi

# Move results to our directory
mv ttft_assessment_results/* "$RESULTS_DIR/"
rmdir ttft_assessment_results

echo
echo "=============================================="
echo "Performance Analysis Summary"
echo "=============================================="

# Analyze the patterns
echo "1. IDENTICAL REQUESTS PATTERN:"
if [ -f "$RESULTS_DIR/identical_requests_ttft.txt" ]; then
    IDENTICAL_AVG=$(grep -v "FAILED\|timeout" "$RESULTS_DIR/identical_requests_ttft.txt" | awk '{
        total += $1; count++
    } END {
        if (count > 0) printf "%.0f", total/count*1000
        else print "N/A"
    }')
    echo "   Average TTFT: ${IDENTICAL_AVG}ms"
    echo "   Expected: First request slower (model loading), subsequent faster (reuse)"
else
    echo "   No results found"
fi

echo
echo "2. VARYING NUMCTX PATTERN:"
if [ -f "$RESULTS_DIR/varying_numctx_ttft.txt" ]; then
    VARYING_AVG=$(grep -v "FAILED\|timeout" "$RESULTS_DIR/varying_numctx_ttft.txt" | awk '{
        total += $1; count++
    } END {
        if (count > 0) printf "%.0f", total/count*1000
        else print "N/A"
    }')
    echo "   Average TTFT: ${VARYING_AVG}ms"
    echo "   Expected: High latency due to model reloading for different KV cache sizes"
else
    echo "   No results found"
fi

echo
echo "3. VARYING LENGTH PATTERN:"
if [ -f "$RESULTS_DIR/varying_length_ttft.txt" ]; then
    VARYING_LENGTH_AVG=$(grep -v "FAILED\|timeout" "$RESULTS_DIR/varying_length_ttft.txt" | awk '{
        total += $1; count++
    } END {
        if (count > 0) printf "%.0f", total/count*1000
        else print "N/A"
    }')
    echo "   Average TTFT: ${VARYING_LENGTH_AVG}ms"
    echo "   Expected: Variable latency due to dynamic NumCtx calculation differences"
else
    echo "   No results found"
fi

echo
echo "4. MIXED PATTERN:"
if [ -f "$RESULTS_DIR/mixed_pattern_ttft.txt" ]; then
    MIXED_AVG=$(grep -v "FAILED\|timeout" "$RESULTS_DIR/mixed_pattern_ttft.txt" | awk '{
        total += $1; count++
    } END {
        if (count > 0) printf "%.0f", total/count*1000
        else print "N/A"
    }')
    echo "   Average TTFT: ${MIXED_AVG}ms"
    echo "   Expected: Realistic usage showing combined impact"
else
    echo "   No results found"
fi

echo
echo "=============================================="
echo "Performance Impact Analysis"
echo "=============================================="

# Calculate overhead
if [ "$IDENTICAL_AVG" != "N/A" ] && [ "$VARYING_AVG" != "N/A" ]; then
    OVERHEAD=$(echo "$VARYING_AVG - $IDENTICAL_AVG" | bc 2>/dev/null || echo "calc_error")
    if [ "$OVERHEAD" != "calc_error" ]; then
        OVERHEAD_PCT=$(echo "scale=1; ($OVERHEAD / $IDENTICAL_AVG) * 100" | bc 2>/dev/null || echo "calc_error")
        echo "Model Reload Overhead: ${OVERHEAD}ms (${OVERHEAD_PCT}% increase)"
    else
        echo "Model Reload Overhead: Could not calculate"
    fi
else
    echo "Model Reload Overhead: Insufficient data"
fi

echo
echo "=============================================="
echo "Server Log Analysis Instructions"
echo "=============================================="
echo "To correlate TTFT spikes with server activity, check the Ollama server logs for:"
echo "1. MODEL_LOAD_START/MODEL_LOAD_SUCCESS patterns"
echo "2. load_duration_ms values in the logs"
echo "3. CACHE_CREATE_START/CACHE_CREATE_COMPLETE patterns"
echo "4. cache_create_duration_ms values"
echo "5. MODEL_UNLOAD_COMPLETE events"
echo
echo "Example log analysis commands:"
echo "  # Filter model loading events"
echo "  grep 'MODEL_LOAD_START\\|MODEL_LOAD_SUCCESS' /path/to/ollama.log"
echo
echo "  # Filter cache creation events"
echo "  grep 'CACHE_CREATE_START\\|CACHE_CREATE_COMPLETE' /path/to/ollama.log"
echo
echo "  # Extract timing data"
echo "  grep 'load_duration_ms\\|cache_create_duration_ms' /path/to/ollama.log"

echo
echo "=============================================="
echo "Diagnostic Recommendations"
echo "=============================================="

if [ "$IDENTICAL_AVG" != "N/A" ] && [ "$VARYING_AVG" != "N/A" ]; then
    if [ $(echo "$VARYING_AVG > $IDENTICAL_AVG * 2" | bc 2>/dev/null || echo "0") -eq 1 ]; then
        echo "⚠️  HIGH PERFORMANCE IMPACT DETECTED"
        echo "   - Model reloading appears to cause significant TTFT degradation"
        echo "   - Consider implementing model caching strategies"
        echo "   - Review per-request unloading necessity"
    elif [ $(echo "$VARYING_AVG > $IDENTICAL_AVG * 1.5" | bc 2>/dev/null || echo "0") -eq 1 ]; then
        echo "⚠️  MODERATE PERFORMANCE IMPACT DETECTED"
        echo "   - Model reloading has measurable impact on TTFT"
        echo "   - Monitor production workloads for impact"
    else
        echo "✅ LOW PERFORMANCE IMPACT"
        echo "   - Model reloading overhead appears acceptable"
    fi
else
    echo "❓ INSUFFICIENT DATA"
    echo "   - Unable to calculate performance impact"
    echo "   - Check if tests completed successfully"
fi

echo
echo "=============================================="
echo "Next Steps"
echo "=============================================="
echo "1. Review the detailed results in: $RESULTS_DIR/"
echo "2. Correlate findings with server logs"
echo "3. If high impact detected, consider:"
echo "   - Implementing intelligent model caching"
echo "   - Adjusting grace periods for model unloading"
echo "   - Optimizing KV cache initialization"
echo "4. Document findings in ISSUE.md and update PLAN.md"

echo
echo "Assessment complete. Results saved to: $RESULTS_DIR/"