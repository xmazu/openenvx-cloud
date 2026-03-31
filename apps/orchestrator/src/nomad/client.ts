export interface NomadDispatchResponse {
  EvalID: string;
  EvalCreateIndex: number;
  JobCreateIndex: number;
  Index: number;
}

export interface NomadEvaluationResponse {
  ID: string;
  Status: string;
  Priority: number;
  Type: string;
  JobID: string;
  JobModifyIndex: number;
  NodeID: string;
  NodeModifyIndex: number;
  StatusDescription: string;
  WaitUntil: number;
  NextEval: string;
  PreviousEval: string;
  CreateIndex: number;
  ModifyIndex: number;
}

export interface NomadClientOptions {
  maxRetries?: number;
  retryDelayMs?: number;
}

export class NomadClient {
  private baseUrl: string;
  private token?: string;
  private maxRetries: number;
  private retryDelayMs: number;

  constructor(
    baseUrl: string,
    token?: string,
    options: NomadClientOptions = {},
  ) {
    this.baseUrl = baseUrl.replace(/\/$/, "");
    this.token = token;
    this.maxRetries = options.maxRetries ?? 3;
    this.retryDelayMs = options.retryDelayMs ?? 1000;
  }

  private getHeaders(): HeadersInit {
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
    };
    if (this.token) {
      headers["X-Nomad-Token"] = this.token;
    }
    return headers;
  }

  private async fetchWithRetry(
    url: string,
    options: RequestInit,
  ): Promise<Response> {
    let lastError: Error | unknown;

    for (let attempt = 0; attempt <= this.maxRetries; attempt++) {
      try {
        const response = await fetch(url, options);

        if (!response.ok) {
          const text = await response.text().catch(() => "");
          throw new Error(
            `HTTP Error: ${response.status} ${response.statusText} - ${text}`,
          );
        }

        return response;
      } catch (error) {
        lastError = error;
        if (attempt < this.maxRetries) {
          await new Promise((resolve) =>
            setTimeout(resolve, this.retryDelayMs),
          );
        }
      }
    }

    throw lastError;
  }

  public async dispatchJob(
    jobId: string,
    payload: Record<string, string>,
  ): Promise<{ evalId: string; evalCreateIndex: number }> {
    const url = `${this.baseUrl}/v1/job/${encodeURIComponent(jobId)}/dispatch`;

    const body = {
      Meta: payload,
    };

    const response = await this.fetchWithRetry(url, {
      method: "POST",
      headers: this.getHeaders(),
      body: JSON.stringify(body),
    });

    const data = (await response.json()) as NomadDispatchResponse;

    return {
      evalId: data.EvalID,
      evalCreateIndex: data.EvalCreateIndex,
    };
  }

  public async parseJobHcl(hcl: string): Promise<any> {
    const url = `${this.baseUrl}/v1/jobs/parse`;

    const response = await this.fetchWithRetry(url, {
      method: "POST",
      headers: this.getHeaders(),
      body: JSON.stringify({
        JobHCL: hcl,
        Canonicalize: true,
      }),
    });

    return await response.json();
  }

  public async registerJob(jobJson: any): Promise<void> {
    const url = `${this.baseUrl}/v1/jobs`;

    await this.fetchWithRetry(url, {
      method: "POST",
      headers: this.getHeaders(),
      body: JSON.stringify({
        Job: jobJson,
      }),
    });
  }

  public async getEvaluationStatus(evalId: string): Promise<string> {
    const url = `${this.baseUrl}/v1/evaluation/${encodeURIComponent(evalId)}`;

    const response = await this.fetchWithRetry(url, {
      method: "GET",
      headers: this.getHeaders(),
    });

    const data = (await response.json()) as NomadEvaluationResponse;
    return data.Status;
  }
}
