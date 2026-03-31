import { describe, it, expect, beforeAll, afterAll, afterEach } from "bun:test";
import { setupServer } from "msw/node";
import { http, HttpResponse } from "msw";
import { NomadClient } from "../nomad/client.js";

const NOMAD_URL = "http://localhost:4646";

const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe("NomadClient", () => {
  it("should successfully dispatch a job and return eval details", async () => {
    server.use(
      http.post(
        `${NOMAD_URL}/v1/job/test-job/dispatch`,
        async ({ request }) => {
          const authHeader = request.headers.get("X-Nomad-Token");
          expect(authHeader).toBe("secret-token");

          const body = (await request.json()) as any;
          expect(body.Meta).toEqual({ payload_key: "test_value" });

          return HttpResponse.json({
            EvalID: "eval-123",
            EvalCreateIndex: 10,
            JobCreateIndex: 5,
            Index: 10,
          });
        },
      ),
    );

    const client = new NomadClient(NOMAD_URL, "secret-token", {
      maxRetries: 0,
    });
    const result = await client.dispatchJob("test-job", {
      payload_key: "test_value",
    });

    expect(result.evalId).toBe("eval-123");
    expect(result.evalCreateIndex).toBe(10);
  });

  it("should fetch evaluation status", async () => {
    server.use(
      http.get(`${NOMAD_URL}/v1/evaluation/eval-123`, () => {
        return HttpResponse.json({
          ID: "eval-123",
          Status: "complete",
          Priority: 50,
          Type: "batch",
          JobID: "test-job",
          JobModifyIndex: 1,
          NodeID: "node-1",
          NodeModifyIndex: 1,
          StatusDescription: "",
          WaitUntil: 0,
          NextEval: "",
          PreviousEval: "",
          CreateIndex: 10,
          ModifyIndex: 12,
        });
      }),
    );

    const client = new NomadClient(NOMAD_URL, "secret-token", {
      maxRetries: 0,
    });
    const status = await client.getEvaluationStatus("eval-123");

    expect(status).toBe("complete");
  });

  it("should retry on network failures and eventually succeed", async () => {
    let attempts = 0;
    server.use(
      http.get(`${NOMAD_URL}/v1/evaluation/eval-retry`, () => {
        attempts++;
        if (attempts < 3) {
          return new HttpResponse(null, { status: 500 });
        }
        return HttpResponse.json({
          ID: "eval-retry",
          Status: "running",
          Priority: 50,
          Type: "batch",
          JobID: "test-job",
          JobModifyIndex: 1,
          NodeID: "node-1",
          NodeModifyIndex: 1,
          StatusDescription: "",
          WaitUntil: 0,
          NextEval: "",
          PreviousEval: "",
          CreateIndex: 10,
          ModifyIndex: 12,
        });
      }),
    );

    const client = new NomadClient(NOMAD_URL, "secret-token", {
      maxRetries: 3,
      retryDelayMs: 10,
    });
    const status = await client.getEvaluationStatus("eval-retry");

    expect(status).toBe("running");
    expect(attempts).toBe(3);
  });

  it("should throw after exhausting max retries", async () => {
    let attempts = 0;
    server.use(
      http.get(`${NOMAD_URL}/v1/evaluation/eval-fail`, () => {
        attempts++;
        return new HttpResponse(null, { status: 500 });
      }),
    );

    const client = new NomadClient(NOMAD_URL, "secret-token", {
      maxRetries: 2,
      retryDelayMs: 10,
    });

    await expect(client.getEvaluationStatus("eval-fail")).rejects.toThrow(
      "HTTP Error: 500",
    );
    expect(attempts).toBe(3);
  });
});
