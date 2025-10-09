// find host/ -name "*.go" -exec grep Export {} /dev/null \;

// cache
export declare function set_string(offset: usize, size: usize): u64;
export declare function get_string(offset: usize, size: usize): u64;
export declare function set_int(offset: usize, size: usize): u64;
export declare function get_int(offset: usize, size: usize): u64;
export declare function set_float(offset: usize, size: usize): u64;
export declare function get_float(offset: usize, size: usize): u64;
export declare function set_bytes(offset: usize, size: usize): u64;
export declare function get_bytes(offset: usize, size: usize): u64;
export declare function remove(offset: usize, size: usize): u64;
export declare function has(offset: usize, size: usize): u64;

// http
export declare function get(offset: usize, size: usize): u64;
export declare function post(offset: usize, size: usize): u64;
export declare function put(offset: usize, size: usize): u64;
// export declare function delete(offset: usize, size: usize): u64
export declare function patch(offset: usize, size: usize): u64;
export declare function head(offset: usize, size: usize): u64;
export declare function options(offset: usize, size: usize): u64;

// config
export declare function get_plugin_config(offset: usize, size: usize): u64;

// websocket
export declare function connect(offset: usize, size: usize): u64;
export declare function send_text(offset: usize, size: usize): u64;
export declare function send_binary(offset: usize, size: usize): u64;
export declare function close(offset: usize, size: usize): u64;
