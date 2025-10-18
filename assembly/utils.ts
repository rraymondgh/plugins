import { Protobuf } from "as-proto/assembly";
import { Reader } from "as-proto/assembly/Reader";
import { Writer } from "as-proto/assembly/Writer";

export namespace u64return {
  // pointer, length and error bit are combined into 64-bit value for go-plugin
  function encodedReturn(
    ptr: usize,
    len: usize,
    error_bit: boolean = false
  ): u64 {
    if (error_bit) {
      len = len | (1 << 31);
    }
    return (u64(ptr) << 32) | u64(len);
  }

  export function errorString(msg: string): u64 {
    const buff = Uint8Array.wrap(String.UTF8.encode(msg));
    return error(buff);
  }

  export function error(msg: Uint8Array): u64 {
    return encodedReturn(msg.dataStart, msg.byteLength, true);
  }

  export function fromMemory(ptr: usize, size: u32): Uint8Array {
    const buff = new Uint8Array(size);
    memory.copy(buff.dataStart, ptr, size);
    return buff;
  }

  export function returnHost(data: Uint8Array): u64 {
    return encodedReturn(data.dataStart, data.length);
  }
}

export namespace pluginproto {
  export class returnHelper<TResponse> {
    error: boolean;
    ptr: usize;

    constructor(error: boolean, ptr: usize) {
      this.error = error;
      this.ptr = ptr;
    }

    errorDetail(): Uint8Array {
      return changetype<Uint8Array>(this.ptr);
    }

    response(): TResponse {
      return changetype<TResponse>(this.ptr);
    }
  }

  export class Plugin {
    static request<TRequest>(
      ptr: usize,
      size: u32,
      decoder: (reader: Reader, length: i32) => TRequest
    ): TRequest {
      return Protobuf.decode<TRequest>(
        u64return.fromMemory(ptr, size),
        decoder
      );
    }

    static response<TResponse>(
      message: TResponse,
      encoder: (message: TResponse, writer: Writer) => void
    ): u64 {
      return u64return.returnHost(Protobuf.encode<TResponse>(message, encoder));
    }

    static host<TRequest, TResponse>(
      hostFunc: (offset: usize, size: usize) => u64,
      request: TRequest,
      encoder: (message: TRequest, writer: Writer) => void,
      decoder: (reader: Reader, length: i32) => TResponse
    ): returnHelper<TResponse> {
      const data = Protobuf.encode<TRequest>(request, encoder);
      const ptrSize = hostFunc(data.dataStart, data.length);
      const ptr = u32(ptrSize >> 32);
      var error = false;
      var size = u32(ptrSize);
      if ((size & (1 << 31)) > 0) {
        error = true;
        size = size & ~(1 << 31);
      }

      if (error) {
        return new returnHelper<TResponse>(
          error,
          changetype<usize>(u64return.fromMemory(ptr, size))
        );
      } else {
        return new returnHelper<TResponse>(
          error,
          changetype<usize>(Plugin.request<TResponse>(ptr, size, decoder))
        );
      }
    }
  }
}
