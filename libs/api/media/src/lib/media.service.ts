import { Injectable, InternalServerErrorException, Logger } from '@nestjs/common';
import { ConfigService } from '@nestjs/config';
import {
  CreateBucketCommand,
  DeleteObjectCommand,
  HeadBucketCommand,
  PutBucketPolicyCommand,
  S3Client,
} from '@aws-sdk/client-s3';
import { Upload } from '@aws-sdk/lib-storage';
import { randomUUID } from 'crypto';

@Injectable()
export class MediaService {
  private readonly logger = new Logger(MediaService.name);
  private readonly s3Client: S3Client;
  private readonly bucket: string;
  private readonly publicBaseUrl: string;
  private readonly bucketReady: Promise<void>;

  constructor(private readonly configService: ConfigService) {
    const endpoint = this.configService.get<string>('MINIO_ENDPOINT', 'http://minio:9000');
    const accessKeyId = this.configService.get<string>('MINIO_ACCESS_KEY', 'minioadmin');
    const secretAccessKey = this.configService.get<string>('MINIO_SECRET_KEY', 'minioadmin');
    const region = this.configService.get<string>('MINIO_REGION', 'us-east-1');

    this.bucket = this.configService.get<string>('MINIO_BUCKET', 'images');
    this.publicBaseUrl = this.configService.get<string>('MINIO_PUBLIC_BASE_URL', '');

    this.s3Client = new S3Client({
      endpoint,
      region,
      credentials: { accessKeyId, secretAccessKey },
      forcePathStyle: true,
    });

    this.bucketReady = this.ensureBucketExists();
  }

  async uploadImage(file: Express.Multer.File): Promise<{ url: string }> {
    const ext = (file.originalname.split('.').pop() ?? 'jpg').toLowerCase();
    const key = `${Date.now()}-${randomUUID()}.${ext}`;

    try {
      await this.bucketReady;

      const upload = new Upload({
        client: this.s3Client,
        params: {
          Bucket: this.bucket,
          Key: key,
          Body: file.buffer,
          ContentType: file.mimetype,
        },
      });

      await upload.done();
    } catch (error) {
      this.logger.error('MinIO upload failed', error);
      throw new InternalServerErrorException('Αποτυχία ανεβάσματος εικόνας. Δοκιμάστε ξανά.');
    }

    const normalizedBase = this.publicBaseUrl.trim().replace(/\/$/, '');
    const url = normalizedBase ? `${normalizedBase}/media/images/${key}` : `/media/images/${key}`;

    return { url };
  }

  extractManagedImageKey(url: string | undefined): string | null {
    const value = url?.trim();
    if (!value) return null;

    let parsed: URL;
    try {
      parsed = new URL(value, 'http://localhost');
    } catch {
      return null;
    }

    const prefix = '/media/images/';
    if (!parsed.pathname.startsWith(prefix)) {
      return null;
    }

    const key = parsed.pathname.slice(prefix.length).trim();
    return key || null;
  }

  async deleteManagedImageByUrl(url: string | undefined): Promise<void> {
    const key = this.extractManagedImageKey(url);
    if (!key) return;

    try {
      await this.bucketReady;
      await this.s3Client.send(
        new DeleteObjectCommand({
          Bucket: this.bucket,
          Key: key,
        }),
      );
    } catch (error) {
      this.logger.warn(`Failed to delete managed image object ${key}`, error as Error);
    }
  }

  private async ensureBucketExists(): Promise<void> {
    try {
      await this.s3Client.send(new HeadBucketCommand({ Bucket: this.bucket }));
    } catch {
      try {
        await this.s3Client.send(new CreateBucketCommand({ Bucket: this.bucket }));
      } catch (error) {
        this.logger.error(`Failed to initialize MinIO bucket: ${this.bucket}`, error);
        throw new InternalServerErrorException('Αποτυχία αρχικοποίησης αποθήκευσης αρχείων.');
      }
    }

    await this.ensurePublicReadPolicy();
  }

  private async ensurePublicReadPolicy(): Promise<void> {
    const policy = {
      Version: '2012-10-17',
      Statement: [
        {
          Sid: 'AllowPublicReadForImages',
          Effect: 'Allow',
          Principal: '*',
          Action: ['s3:GetObject'],
          Resource: [`arn:aws:s3:::${this.bucket}/*`],
        },
      ],
    };

    try {
      await this.s3Client.send(
        new PutBucketPolicyCommand({
          Bucket: this.bucket,
          Policy: JSON.stringify(policy),
        }),
      );
    } catch (error) {
      this.logger.error(`Failed to apply public-read policy for bucket: ${this.bucket}`, error);
      throw new InternalServerErrorException('Αποτυχία ρύθμισης πρόσβασης εικόνων.');
    }
  }
}
