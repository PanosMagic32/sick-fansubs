import { Module } from '@nestjs/common';
import { ConfigModule, ConfigService } from '@nestjs/config';
import { MongooseModule } from '@nestjs/mongoose';

import { ApiAuthModule } from '@sick/api/auth';
import { ApiBlogPostModule } from '@sick/api/blog-post';
import { ApiProjectModule } from '@sick/api/project';
import { ApiUserModule } from '@sick/api/user';

import { environment } from '../environments/environment';

@Module({
  imports: [
    ApiBlogPostModule,
    ApiUserModule,
    ApiProjectModule,
    ApiAuthModule,
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
})
export class AppModule {}
