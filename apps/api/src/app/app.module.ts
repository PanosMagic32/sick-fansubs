import { Module } from '@nestjs/common';
import { MongooseModule } from '@nestjs/mongoose';
import { ConfigModule, ConfigService } from '@nestjs/config';

import { ApiBlogPostModule } from '@sick/api/blog-post';
// import { ApiUserModule } from '@sick/api/user';
import { ApiProjectModule } from '@sick/api/project';
// import { ApiAuthModule } from '@sick/api/auth';

@Module({
    imports: [
        ApiBlogPostModule,
        // ApiUserModule,
        ApiProjectModule,
        // ApiAuthModule,
        ConfigModule.forRoot({
            isGlobal: true,
        }),
        // MongooseModule.forRoot(process.env.DATABASE_URL, { useNewUrlParser: true, useUnifiedTopology: true }),
        MongooseModule.forRootAsync({
            imports: [ConfigModule],
            useFactory: async (configService: ConfigService) => ({
                uri: configService.get<string>('DATABASE_URL'),
                dbName: 'sick-db',
                useNewUrlParser: true,
                useUnifiedTopology: true,
            }),
            inject: [ConfigService],
        }),
    ],
})
export class AppModule {}
