import { Module } from '@nestjs/common';
import { APP_GUARD } from '@nestjs/core';
import { ConfigModule, ConfigService } from '@nestjs/config';
import { MongooseModule } from '@nestjs/mongoose';
import { ThrottlerGuard, ThrottlerModule } from '@nestjs/throttler';

import { ApiAuthModule } from '@api/auth';
import { ApiBlogPostModule } from '@api/blog-post';
import { ApiMediaModule } from '@api/media';
import { ApiProjectModule } from '@api/project';
import { ApiSearchModule } from '@api/search';
import { ApiUserModule } from '@api/user';

import { environment } from '../environments/environment';
import { HealthController } from './health.controller';

@Module({
  imports: [
    ApiBlogPostModule,
    ApiUserModule,
    ApiProjectModule,
    ApiAuthModule,
    ApiSearchModule,
    ApiMediaModule,
    ConfigModule.forRoot({
      isGlobal: true,
    }),
    ThrottlerModule.forRoot([
      {
        ttl: 60000,
        limit: 120,
      },
    ]),
    MongooseModule.forRootAsync({
      imports: [ConfigModule],
      useFactory: async (configService: ConfigService) => ({
        uri: configService.get<string>(environment.production ? 'DATABASE_URL' : 'DATABASE_URL_DEV'),
        dbName: environment.production ? 'sick-db' : 'dev-sick-db',
      }),
      inject: [ConfigService],
    }),
  ],
  controllers: [HealthController],
  providers: [
    {
      provide: APP_GUARD,
      useClass: ThrottlerGuard,
    },
  ],
})
export class AppModule {}
