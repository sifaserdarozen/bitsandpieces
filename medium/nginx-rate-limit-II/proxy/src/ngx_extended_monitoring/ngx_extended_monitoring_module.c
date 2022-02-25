#include <ngx_config.h>
#include <ngx_core.h>
#include <ngx_http.h>


// response codes can be seen from
// https://www.nginx.com/resources/wiki/extending/api/http/
#define LAST_HTTP_RESPONSE_CODE NGX_HTTP_INSUFFICIENT_STORAGE
#define FIRST_HTTP_RESPONSE_CODE NGX_HTTP_CONTINUE

#define NUMBER_OF_RESPONSE_CODES (LAST_HTTP_RESPONSE_CODE - FIRST_HTTP_RESPONSE_CODE + 1)

// one additinal item will cary information about "other" counter
#define RESPONSE_HISTOGRAM_SIZE (NUMBER_OF_RESPONSE_CODES + 1)

ngx_atomic_int_t* responseCodes[RESPONSE_HISTOGRAM_SIZE];

// interested in count of only these response codes
static const ngx_int_t interestedResponseCodes[] = {
    NGX_HTTP_OK,
    NGX_HTTP_MOVED_TEMPORARILY,
    NGX_HTTP_SEE_OTHER,
    NGX_HTTP_NOT_FOUND,
    NGX_HTTP_SERVICE_UNAVAILABLE
};

static const int numberOfInterestedCodes = sizeof(interestedResponseCodes) / sizeof(interestedResponseCodes[0]);

// init and cleanup code
static ngx_int_t ngxExtendedMonitoringInit(ngx_conf_t* conf);
static void ngxExtendedMonitoringCleanup(ngx_cycle_t* cycle);

static char* ngxExtendedMonitoring(ngx_conf_t* cf, ngx_command_t* cmd, void* conf);
static ngx_int_t ngxExtendedMonitoringHandler(ngx_http_request_t* req);

ngx_shm_t extendedMonitoring;

// Specify module directive
static ngx_command_t ngxExtendedMonitoringCommands[] = {
    { ngx_string("extended_monitoring"),                    // directive
      NGX_HTTP_SRV_CONF|NGX_HTTP_LOC_CONF|NGX_CONF_FLAG,    // specify directive options
      ngxExtendedMonitoring,                                // configuration setup function
      0,                                                    // indicate that there is one context
      0,                                                    // module configuration is stored with no offset
      NULL },

    ngx_null_command                                        // indicate command termination
};

// Extended monitoring module context
static ngx_http_module_t ngxExtendedMonitoringModuleCtx = {
    NULL,                       // preconfiguration
    ngxExtendedMonitoringInit,  // postconfiguration
    NULL,                       // create main configuration
    NULL,                       // init main configuration
    NULL,                       // create server configuration
    NULL,                       // merge server configuration
    NULL,                       // create location configuration
    NULL                        // merge location configuration
};

// Extended monitoring module definition
ngx_module_t ngx_extended_monitoring_module = {
    NGX_MODULE_V1,
    &ngxExtendedMonitoringModuleCtx,        // module context that will keep data
    ngxExtendedMonitoringCommands,          // module directives
    NGX_HTTP_MODULE,                        // module type
    NULL,                                   // init master
    NULL,                                   // init module
    NULL,                                   // init process
    NULL,                                   // init thread
    NULL,                                   // exit thread
    NULL,                                   // exit process
    ngxExtendedMonitoringCleanup,           // exit mastercleanup
    NGX_MODULE_V1_PADDING
};

// Content handler.
static ngx_int_t ngxExtendedMonitoringHandler(ngx_http_request_t* req)
{
    ngx_atomic_int_t code_counts[RESPONSE_HISTOGRAM_SIZE];

    if (req->method != NGX_HTTP_GET && req->method != NGX_HTTP_HEAD) {
        return NGX_HTTP_NOT_ALLOWED;
    }

    ngx_int_t rc = ngx_http_discard_request_body(req);

    if (NGX_OK != rc) {
        return rc;
    }

    for (int i = 0; i < RESPONSE_HISTOGRAM_SIZE; i++) {
        code_counts[i] = *responseCodes[i];
    }

    ngx_str_set(&req->headers_out.content_type, "application/json");

    if (req->method == NGX_HTTP_HEAD) {
        req->headers_out.status = NGX_HTTP_OK;

        rc = ngx_http_send_header(req);

        if (rc == NGX_ERROR || rc > NGX_OK || req->header_only) {
            return rc;
        }
    }

    // "200":1,
    const size_t sizeOfAnInterestedCodeLine = sizeof("\"XXX\":") + NGX_ATOMIC_T_LEN + sizeof(",");
    // "other":0
    const size_t sizeOfOtherCodeLine = sizeof("\"other\":") + NGX_ATOMIC_T_LEN;
    const size_t size = sizeof("{}") + numberOfInterestedCodes * sizeOfAnInterestedCodeLine + sizeOfOtherCodeLine;

    ngx_buf_t* buffer = ngx_create_temp_buf(req->pool, size);
    if (NULL == buffer) {
        return NGX_HTTP_INTERNAL_SERVER_ERROR;
    }

    // response will be in json
    // {"200":1,"302":0,"303":0,"404":2,"503":0,"other":0}
    buffer->last = ngx_sprintf(buffer->last, "{");
    for (int i = 0; i < numberOfInterestedCodes; i++ ) {
        buffer->last = ngx_sprintf(buffer->last, "\"%uA\":%uA,", interestedResponseCodes[i], code_counts[interestedResponseCodes[i] - FIRST_HTTP_RESPONSE_CODE]);
    }
    buffer->last = ngx_sprintf(buffer->last, "\"other\":%uA", code_counts[RESPONSE_HISTOGRAM_SIZE - 1]);
    buffer->last = ngx_sprintf(buffer->last, "}");

    req->headers_out.status = NGX_HTTP_OK;
    req->headers_out.content_length_n = buffer->last - buffer->pos;

    buffer->last_buf = 1;

    rc = ngx_http_send_header(req);

    if (rc == NGX_ERROR || rc > NGX_OK || req->header_only) {
        return rc;
    }

    ngx_chain_t out;
    out.buf = buffer;
    out.next = NULL;

    return ngx_http_output_filter(req, &out);
} 

// Configuration setup function that installs the content handler.
static char* ngxExtendedMonitoring(ngx_conf_t* conf, ngx_command_t* cmd, void* voidConf) {
    /* Install the hello world handler. */
    ngx_http_core_loc_conf_t* clcf = ngx_http_conf_get_module_loc_conf(conf, ngx_http_core_module);
    clcf->handler = ngxExtendedMonitoringHandler;

    return NGX_CONF_OK;
}

ngx_int_t ngxRequestCountHandler(ngx_http_request_t* req) {
    if (req->headers_out.status >= FIRST_HTTP_RESPONSE_CODE && req->headers_out.status <= LAST_HTTP_RESPONSE_CODE) {
        const ngx_int_t idx = req->headers_out.status - FIRST_HTTP_RESPONSE_CODE;
        // this part can be iterated against overflow
        if (-1 != *(responseCodes[idx])) {
            ngx_atomic_fetch_add(responseCodes[idx], 1);
        } else {
            ngx_atomic_fetch_add(responseCodes[RESPONSE_HISTOGRAM_SIZE - 1], 1);
        }
    }

    return NGX_OK;
}

// Initialization code
static ngx_int_t ngxExtendedMonitoringInit(ngx_conf_t* conf) {
    const size_t ngxAtomicSize = sizeof(ngx_atomic_int_t);

    extendedMonitoring.size = ngxAtomicSize * RESPONSE_HISTOGRAM_SIZE;
    extendedMonitoring.name.data = (u_char*)"ngx_code_counts";
    extendedMonitoring.name.len = sizeof("ngx_code_counts");
    extendedMonitoring.log = conf->log;

    if (NGX_OK != ngx_shm_alloc(&extendedMonitoring)) {
        return NGX_ERROR;
    }

    u_char* shared = extendedMonitoring.addr;

    // set histogram initial value to -1 to indicate we are not interested
    for (ngx_int_t i = 0; i < RESPONSE_HISTOGRAM_SIZE; ++i) {
        responseCodes[i] = (ngx_atomic_int_t *)(shared + ngxAtomicSize * i);
        *responseCodes[i] = -1;
    }

    // set the interested counters to 0
    for (int i = 0; i < numberOfInterestedCodes; ++i) {
        *responseCodes[interestedResponseCodes[i] - FIRST_HTTP_RESPONSE_CODE] = 0;
    }

    // the last value will indicate the "other" field
    *responseCodes[RESPONSE_HISTOGRAM_SIZE - 1] = 0;

    ngx_http_core_main_conf_t* mainConf = ngx_http_conf_get_module_main_conf(conf, ngx_http_core_module);

    ngx_http_handler_pt* handler = ngx_array_push(&mainConf->phases[NGX_HTTP_LOG_PHASE].handlers);

    if (NULL == handler) {
        return NGX_ERROR;
    }

    *handler = ngxRequestCountHandler;

    return NGX_OK;
}

// Cleanup code
static void ngxExtendedMonitoringCleanup(ngx_cycle_t* cycle) {
    ngx_shm_free(&extendedMonitoring);
}
