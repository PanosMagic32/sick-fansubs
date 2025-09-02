import { Prop, Schema, SchemaFactory } from '@nestjs/mongoose';
import mongoose, { Document } from 'mongoose';

import { User } from '@sick/api/user';

export type BlogPostDocument = BlogPost & Document;

@Schema({ collection: 'blogposts' })
export class BlogPost {
  @Prop({ required: true })
  title!: string;

  @Prop({ required: true })
  subtitle!: string;

  @Prop({ required: true })
  description!: string;

  @Prop({ required: true })
  thumbnail!: string;

  @Prop({ required: true })
  downloadLink!: string;

  @Prop()
  downloadLink4k!: string;

  @Prop({ required: true })
  dateTimeCreated!: string;

  @Prop({ type: mongoose.Schema.Types.ObjectId, ref: 'User' })
  creator!: User;
}

export const BlogPostSchema = SchemaFactory.createForClass(BlogPost);
