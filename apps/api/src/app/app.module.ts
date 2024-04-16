import { Module } from '@nestjs/common';
import { MongooseModule } from '@nestjs/mongoose';
import { ConfigModule, ConfigService } from '@nestjs/config';

import { ApiBlogPostModule } from '@sick/api/blog-post';
import { ApiUserModule } from '@sick/api/user';
import { ApiProjectModule } from '@sick/api/project';
import { ApiAuthModule } from '@sick/api/auth';

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
        useNewUrlParser: true,
        useUnifiedTopology: true,
      }),
      inject: [ConfigService],
    }),
  ],
})
export class AppModule {
  constructor() {
    console.log(environment);
  }
}
