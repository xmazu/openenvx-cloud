import { spawn } from 'node:child_process';
import { PassThrough } from 'node:stream';
import { S3Client } from '@aws-sdk/client-s3';
import { Upload } from '@aws-sdk/lib-storage';

async function main() {
  const args = process.argv.slice(2);
  let jobId = process.env.NOMAD_META_JOB_ID;
  let commandArgs: string[] = [];

  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (!arg) continue;
    
    if (arg === '--job-id') {
      const nextArg = args[++i];
      if (nextArg) jobId = nextArg;
    } else if (arg.startsWith('--job-id=')) {
      jobId = arg.split('=')[1];
    } else if (arg === '--') {
      for (const a of args.slice(i + 1)) {
        if (a) commandArgs.push(a);
      }
      break;
    } else {
      commandArgs.push(arg);
    }
  }

  if (!jobId) {
    console.error("Missing --job-id or NOMAD_META_JOB_ID");
    process.exit(1);
  }

  if (commandArgs.length === 0) {
    console.error("Missing command to execute");
    process.exit(1);
  }

  const command = commandArgs[0] as string;
  const cmdArgs = commandArgs.slice(1);

  const endpoint = process.env.S3_ENDPOINT;
  const s3Client = new S3Client({
    region: process.env.AWS_REGION || 'us-east-1',
    endpoint: endpoint,
    forcePathStyle: !!endpoint,
  });

  const passThrough = new PassThrough();

  const upload = new Upload({
    client: s3Client,
    params: {
      Bucket: 'terraform-logs',
      Key: `logs/${jobId}/execution.log`,
      Body: passThrough,
      ContentType: 'text/plain',
    },
  });

  const uploadPromise = upload.done().catch((err: unknown) => {
    console.error("Failed to upload logs:", err);
  });

  const child = spawn(command, cmdArgs, {
    stdio: ['ignore', 'pipe', 'pipe'] as const,
  });

  child.stdout.on('data', (chunk: Buffer) => {
    process.stdout.write(chunk);
    passThrough.write(chunk);
  });

  child.stderr.on('data', (chunk: Buffer) => {
    process.stderr.write(chunk);
    passThrough.write(chunk);
  });

  child.on('close', async (code: number | null) => {
    passThrough.end();
    await uploadPromise;
    process.exit(code ?? 1);
  });
  
  child.on('error', (err: Error) => {
    console.error("Failed to start child process:", err);
    passThrough.end(`Error: ${err.message}\n`);
    uploadPromise.finally(() => {
      process.exit(1);
    });
  });
}

main().catch((err: unknown) => {
  console.error("Unexpected error:", err);
  process.exit(1);
});
