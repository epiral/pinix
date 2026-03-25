// ---- Connect-RPC protocol constants ----

const HUB_PROCEDURES = {
  listClips: "/pinix.v2.HubService/ListClips",
  listProviders: "/pinix.v2.HubService/ListProviders",
  getManifest: "/pinix.v2.HubService/GetManifest",
  invoke: "/pinix.v2.HubService/Invoke",
  getBindings: "/pinix.v2.HubService/GetBindings",
  setBinding: "/pinix.v2.HubService/SetBinding",
  removeBinding: "/pinix.v2.HubService/RemoveBinding",
} as const;

const CONNECT_PROTOCOL_VERSION = "1";
const JSON_HEADERS: Record<string, string> = {
  Accept: "application/json",
  "Content-Type": "application/json",
  "Connect-Protocol-Version": CONNECT_PROTOCOL_VERSION,
};
const STREAM_HEADERS: Record<string, string> = {
  Accept: "application/connect+json",
  "Content-Type": "application/connect+json",
  "Connect-Protocol-Version": CONNECT_PROTOCOL_VERSION,
};

const textEncoder = new TextEncoder();
const textDecoder = new TextDecoder();

// ---- Unary RPC ----

export async function hubUnary(path: string, message: unknown): Promise<Record<string, unknown>> {
  const response = await fetch(path, {
    method: "POST",
    headers: JSON_HEADERS,
    body: JSON.stringify(message || {}),
  });

  const payload = await parseResponsePayload(response);
  if (!response.ok) {
    throw new Error(extractErrorMessage(payload, response));
  }
  return (payload as Record<string, unknown>) || {};
}

// ---- Streaming Invoke ----

export async function hubInvoke(
  clipName: string,
  commandName: string,
  input: unknown,
): Promise<unknown> {
  const response = await fetch(HUB_PROCEDURES.invoke, {
    method: "POST",
    headers: STREAM_HEADERS,
    body: encodeConnectEnvelope({
      clipName,
      command: commandName,
      input: encodeProtoBytes(input),
    }),
  });

  if (!response.ok) {
    const payload = await parseResponsePayload(response);
    throw new Error(extractErrorMessage(payload, response));
  }
  if (!response.body) {
    throw new Error("Streaming response body is unavailable");
  }

  const outputs: Uint8Array[] = [];
  for await (const message of readConnectStream(response.body)) {
    if ((message as Record<string, unknown>)?.error) {
      const err = (message as Record<string, Record<string, string>>).error;
      throw new Error(err.message || "Invocation failed");
    }
    const output = (message as Record<string, unknown>)?.output;
    if (typeof output === "string" && output !== "") {
      outputs.push(decodeBase64Bytes(output));
    }
  }
  return decodeInvocationOutput(outputs);
}

// ---- Procedure paths (re-exported for use in state) ----

export const PROCEDURES = HUB_PROCEDURES;

// ---- Response parsing ----

async function parseResponsePayload(response: Response): Promise<unknown> {
  const text = await response.text();
  if (!text) return null;
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

function extractErrorMessage(payload: unknown, response: Response): string {
  if (payload && typeof payload === "object") {
    const p = payload as Record<string, unknown>;
    if (typeof p.message === "string" && (p.message as string).trim()) {
      return p.message as string;
    }
    if (
      p.error &&
      typeof p.error === "object" &&
      typeof (p.error as Record<string, unknown>).message === "string"
    ) {
      const msg = (p.error as Record<string, string>).message;
      if (msg.trim()) return msg;
    }
  }
  if (typeof payload === "string" && payload.trim()) {
    return payload.trim();
  }
  return response.statusText || "Request failed";
}

// ---- Connect-RPC envelope framing ----

async function* readConnectStream(body: ReadableStream<Uint8Array>): AsyncGenerator<unknown> {
  const reader = body.getReader();
  let buffer = new Uint8Array(0);

  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      if (value && value.length > 0) {
        buffer = concatByteArrays(buffer, value);
      }

      for (;;) {
        const frame = readConnectFrame(buffer);
        if (!frame) break;

        buffer = frame.rest;
        if (frame.compressed) {
          throw new Error("Compressed Connect frames are not supported");
        }

        const payload = parseConnectJSON(frame.payload);
        if (frame.endStream) {
          if (buffer.length > 0) {
            throw new Error(`Corrupt response: ${buffer.length} extra bytes after end of stream`);
          }
          if ((payload as Record<string, unknown>)?.error) {
            const err = (payload as Record<string, Record<string, string>>).error;
            throw new Error(err.message || "Request failed");
          }
          return;
        }

        yield payload;
      }
    }
  } finally {
    reader.releaseLock();
  }

  if (buffer.length > 0) {
    throw new Error("Incomplete Connect frame");
  }
}

interface ConnectFrame {
  compressed: boolean;
  endStream: boolean;
  payload: Uint8Array;
  rest: Uint8Array;
}

function readConnectFrame(buffer: Uint8Array): ConnectFrame | null {
  if (!(buffer instanceof Uint8Array) || buffer.length < 5) return null;

  const view = new DataView(buffer.buffer, buffer.byteOffset, buffer.byteLength);
  const flags = view.getUint8(0);
  const length = view.getUint32(1);
  const totalLength = 5 + length;

  if (buffer.length < totalLength) return null;

  return {
    compressed: Boolean(flags & 0b00000001),
    endStream: Boolean(flags & 0b00000010),
    payload: buffer.slice(5, totalLength),
    rest: buffer.slice(totalLength),
  };
}

function parseConnectJSON(bytes: Uint8Array): unknown {
  const text = textDecoder.decode(bytes);
  if (!text) return {};
  try {
    return JSON.parse(text);
  } catch (error) {
    throw new Error(`Invalid Connect JSON frame: ${(error as Error).message || error}`);
  }
}

function encodeConnectEnvelope(message: unknown): Uint8Array {
  const payload = textEncoder.encode(JSON.stringify(message || {}));
  const frame = new Uint8Array(5 + payload.length);
  const view = new DataView(frame.buffer);
  view.setUint8(0, 0);
  view.setUint32(1, payload.length);
  frame.set(payload, 5);
  return frame;
}

function encodeProtoBytes(value: unknown): string {
  return encodeBase64Bytes(textEncoder.encode(JSON.stringify(value || {})));
}

function decodeInvocationOutput(outputs: Uint8Array[]): unknown {
  if (!outputs.length) return {};
  const bytes = outputs.length === 1 ? outputs[0] : concatManyByteArrays(outputs);
  const text = textDecoder.decode(bytes);
  if (!text.trim()) return {};
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

// ---- Binary helpers ----

function concatByteArrays(left: Uint8Array, right: Uint8Array): Uint8Array {
  if (!left.length) return right.slice();
  if (!right.length) return left.slice();
  const combined = new Uint8Array(left.length + right.length);
  combined.set(left, 0);
  combined.set(right, left.length);
  return combined;
}

function concatManyByteArrays(chunks: Uint8Array[]): Uint8Array {
  let length = 0;
  for (const chunk of chunks) length += chunk.length;
  const combined = new Uint8Array(length);
  let offset = 0;
  for (const chunk of chunks) {
    combined.set(chunk, offset);
    offset += chunk.length;
  }
  return combined;
}

function encodeBase64Bytes(bytes: Uint8Array): string {
  let binary = "";
  const chunkSize = 0x8000;
  for (let i = 0; i < bytes.length; i += chunkSize) {
    const chunk = bytes.subarray(i, i + chunkSize);
    binary += String.fromCharCode(...chunk);
  }
  return btoa(binary);
}

function decodeBase64Bytes(value: string): Uint8Array {
  const binary = atob(value || "");
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}
