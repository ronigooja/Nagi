/* Nagi's deliberately small QuickJS/WASM boundary. */
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include "quickjs.h"

#define OUTPUT_MAX (12 * 1024 * 1024)
static uint32_t output_size;
uint32_t result_size(void) { return output_size; }

/* A fresh WASM instance is discarded after this single call. On every failure
 * return zero, never exception messages, source text, or JavaScript stacks. */
uint32_t transform_json(const char *source, uint32_t source_size,
                        const char *input, uint32_t input_size) {
    output_size = 0;
    JSRuntime *rt = JS_NewRuntime();
    if (!rt) return 0;
    JS_SetMemoryLimit(rt, 120 * 1024 * 1024);
    JS_SetMaxStackSize(rt, 512 * 1024);
    JSContext *ctx = JS_NewContext(rt);
    if (!ctx) return 0;
    JSValue global = JS_GetGlobalObject(ctx);
    /* No clock, entropy, host functions, loaders, or asynchronous jobs. The C
     * standard library (quickjs-libc) is never linked. */
    JS_SetPropertyStr(ctx, global, "Date", JS_UNDEFINED);
    JSValue math = JS_GetPropertyStr(ctx, global, "Math");
    JS_SetPropertyStr(ctx, math, "random", JS_UNDEFINED);
    JS_FreeValue(ctx, math);
    JSValue arg = JS_ParseJSON(ctx, input, input_size, "input");
    if (JS_IsException(arg)) return 0;
    JSValue original = JS_GetPropertyStr(ctx, arg, "original");
    if (!JS_IsUndefined(original)) {
        /* Freeze the complete original metadata graph before user code sees it.
         * The working message remains mutable inside this isolated invocation. */
        const char *freeze_source = "(function freeze(v){if(v&&typeof v==='object'){Object.freeze(v);Object.keys(v).forEach(function(k){freeze(v[k]);});}return v;})";
        JSValue freeze = JS_Eval(ctx, freeze_source, strlen(freeze_source), "freeze", JS_EVAL_TYPE_GLOBAL);
        if (JS_IsException(freeze)) return 0;
        JSValue frozen = JS_Call(ctx, freeze, JS_UNDEFINED, 1, &original);
        if (JS_IsException(frozen)) return 0;
        JS_FreeValue(ctx, frozen);
        if (JS_DefinePropertyValueStr(ctx, arg, "original", JS_DupValue(ctx, original), JS_PROP_ENUMERABLE) < 0) return 0;
        JS_FreeValue(ctx, freeze);
    }
    JS_FreeValue(ctx, original);
    JSValue evaluated = JS_Eval(ctx, source, source_size, "transform", JS_EVAL_TYPE_GLOBAL);
    if (JS_IsException(evaluated)) return 0;
    JS_FreeValue(ctx, evaluated);
    JSValue fn = JS_GetPropertyStr(ctx, global, "transform");
    if (!JS_IsFunction(ctx, fn)) return 0;
    JSValue result = JS_Call(ctx, fn, JS_UNDEFINED, 1, &arg);
    if (JS_IsException(result)) return 0;
    JSValue json = JS_JSONStringify(ctx, result, JS_UNDEFINED, JS_UNDEFINED);
    if (JS_IsException(json) || JS_IsUndefined(json)) return 0;
    size_t n;
    const char *encoded = JS_ToCStringLen(ctx, &n, json);
    if (!encoded || n == 0 || n > OUTPUT_MAX) return 0;
    output_size = (uint32_t)n;
    return (uint32_t)(uintptr_t)encoded;
}
