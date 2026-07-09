import { PartialType } from '@nestjs/swagger';

import { BaseBlogPostDto } from './base-blog-post.dto';

export class UpdateBlogPostDto extends PartialType(BaseBlogPostDto) {}
