import { Module } from '@nestjs/common';
import { MongooseModule } from '@nestjs/mongoose';

import { ApiBlogPostController } from './api-blog-post.controller';
import { ApiBlogPostService } from './api-blog-post.service';
import { BlogPost, BlogPostSchema } from './schemas/blog-post.schema';

@Module({
    imports: [
        // MongooseModule.forFeature([{ name: BlogPost.name, schema: BlogPostSchema }]),
        MongooseModule.forFeatureAsync([
            {
                name: BlogPost.name,
                useFactory: () => {
                    return BlogPostSchema;
                },
            },
        ]),
    ],
    controllers: [ApiBlogPostController],
    providers: [ApiBlogPostService],
})
export class ApiBlogPostModule {}
