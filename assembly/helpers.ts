import { returnHelper, Plugin } from "./utils";
import { get_plugin_config } from "./env";
import { GetPluginConfigRequest } from "./config/GetPluginConfigRequest";
import { GetPluginConfigResponse } from "./config/GetPluginConfigResponse";
import { HttpRequest } from "./http/HttpRequest";
import { HttpResponse } from "./http/HttpResponse";

export class helper {
  static getConfig(): returnHelper<GetPluginConfigResponse> {
    return Plugin.host<GetPluginConfigRequest, GetPluginConfigResponse>(
      get_plugin_config,
      new GetPluginConfigRequest(),
      GetPluginConfigRequest.encode,
      GetPluginConfigResponse.decode
    );
  }

  static http(
    hostFunc: (offset: usize, size: usize) => u64,
    url: string,
    headers: Map<string, string>,
    body: string,
    timeoutMs: i32 = 100
  ): returnHelper<HttpResponse> {
    return Plugin.host<HttpRequest, HttpResponse>(
      hostFunc,
      new HttpRequest(
        url,
        headers,
        timeoutMs,
        Uint8Array.wrap(String.UTF8.encode(body))
      ),
      HttpRequest.encode,
      HttpResponse.decode
    );
  }
}
