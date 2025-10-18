import { pluginproto } from "./utils";
import { get_plugin_config } from "./env";
import { GetPluginConfigRequest } from "./config/GetPluginConfigRequest";
import { GetPluginConfigResponse } from "./config/GetPluginConfigResponse";
import { HttpRequest } from "./http/HttpRequest";
import { HttpResponse } from "./http/HttpResponse";

export namespace helper {
  export function GetConfig(): pluginproto.returnHelper<GetPluginConfigResponse> {
    return pluginproto.Plugin.host<
      GetPluginConfigRequest,
      GetPluginConfigResponse
    >(
      get_plugin_config,
      new GetPluginConfigRequest(),
      GetPluginConfigRequest.encode,
      GetPluginConfigResponse.decode
    );
  }

  export function http(
    hostFunc: (offset: usize, size: usize) => u64,
    url: string,
    headers: Map<string, string>,
    body: string,
    timeoutMs: i32 = -1
  ): pluginproto.returnHelper<HttpResponse> {
    let req = new HttpRequest(url, headers, timeoutMs);
    if (body.length > 0) {
      req.body = Uint8Array.wrap(String.UTF8.encode(body));
    }
    return pluginproto.Plugin.host<HttpRequest, HttpResponse>(
      hostFunc,
      req,
      HttpRequest.encode,
      HttpResponse.decode
    );
  }
}
