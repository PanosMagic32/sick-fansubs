import { Module } from '@nestjs/common';
import { ConfigModule, ConfigService } from '@nestjs/config';
import { MongooseModule } from '@nestjs/mongoose';

import { ApiAuthModule } from '@api/auth';
import { ApiBlogPostModule } from '@api/blog-post';
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
    ConfigModule.forRoot({
      isGlobal: true,
    }),
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
})
export class AppModule {}
