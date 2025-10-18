import { SimpleRequest } from "../../assembly/testapi/SimpleRequest";
import { HttpRequest as Params } from "../../assembly/testapi/HttpRequest";
import { SimpleResponse } from "../../assembly/testapi/SimpleResponse";
import { helper } from "../../assembly/helpers";
import { get } from "../../assembly/env";
import { u64return } from "../../assembly/utils";
import { JSON } from "json-as";

export function integration_test_api_version(): u32 {
  return 1;
}

export function integration_test_config_test(ptr: usize, size: u32): u64 {
  const req = pluginproto.Plugin.request<SimpleRequest>(
    ptr,
    size,
    SimpleRequest.decode
  );

  const config = helper.GetConfig();
  if (config.error) {
    return u64return.error(config.errorDetail());
  }

  // trace("fake_test_as");

  return pluginproto.Plugin.response<SimpleResponse>(
    new SimpleResponse("fake_test_as"),
    SimpleResponse.encode
  );
}

@json
class servarr {
  tvdbId: u64;
  tmdbId: u64;
}

export function integration_test_http_test(ptr: usize, size: u32): u64 {
  const req = pluginproto.Plugin.request<Params>(ptr, size, Params.decode);
  const url = `${req.url}${req.id}`;

  const resp = helper.http(
    get,
    url,
    new Map<string, string>().set("X-Api-Key", req.apikey),
    "",
    -1
  );
  if (resp.error) {
    return u64return.error(resp.errorDetail());
  }
  if (resp.response().status != 200) {
    return u64return.errorString(`[${resp.response().status}] ${url}`);
  }

  const respDecoded = JSON.parse<servarr[]>(
    String.UTF8.decode(resp.response().body.buffer)
  );

  if (respDecoded.length != 1) {
    return u64return.errorString(`bad decoded length: ${respDecoded.length}`);
  }

  return pluginproto.Plugin.response<SimpleResponse>(
    new SimpleResponse(`api result: ${respDecoded[0].tmdbId}`),
    SimpleResponse.encode
  );
}

export function malloc(size: usize): usize {
  return pluginmem.malloc(size);
}

export function free(ptr: usize): void {
  pluginmem.free(ptr);
  return;
}
